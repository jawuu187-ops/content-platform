// Command e2eseed prepares the fixture a device test needs: one fan, one published
// creator with a paid tier, a live membership, and a conversation the creator has
// already spoken in.
//
// It exists so the emulator test exercises the real login, inbox, thread, send and
// tip paths instead of a fake server. It refuses to run anywhere except APP_ENV=demo
// with the fake payment provider, so it can never touch a staging or production
// database, and it writes no money: the membership is an "admin" grant of the kind
// support issues by hand.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/messaging"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
)

const (
	fanEmail      = "fan@e2e.test"
	creatorEmail  = "creator@e2e.test"
	creatorHandle = "mira_e2e"
	creatorName   = "Mira Vale"
)

// password is filled from E2E_PASSWORD at start; it is never written to a log line
// or to the fixture file.
var password string

type allowAll struct{}

func (allowAll) AllowAdult(context.Context, string) (bool, error) { return true, nil }

type seed struct {
	BaseURL        string `json:"base_url"`
	FanEmail       string `json:"fan_email"`
	CreatorID      string `json:"creator_id"`
	CreatorName    string `json:"creator_name"`
	ConversationID string `json:"conversation_id"`
	FanID          string `json:"fan_id"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2eseed:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("json", "", "write the fixture as JSON to this path")
	expire := flag.Bool("expire", false, "end the seeded membership instead of creating one")
	flag.Parse()

	env := os.Getenv("APP_ENV")
	if env != "demo" && env != "local" {
		return fmt.Errorf("refusing to seed: APP_ENV is %q, e2eseed only runs in demo or local", env)
	}
	if os.Getenv("PAY_PROVIDER") != "fake" {
		return errors.New("refusing to seed: PAY_PROVIDER must be fake")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is not set")
	}
	// The password comes from the workflow, not from this file: a literal in a
	// repository is a credential, and the device test is told the same value
	// through an instrumentation argument.
	password = os.Getenv("E2E_PASSWORD")
	if len(password) < 16 {
		return errors.New("E2E_PASSWORD must be set to at least 16 characters")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := database.Open(ctx, database.Options{URL: url, MaxConns: 4, ApplicationName: "e2eseed"})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	pool := db.Pool()
	identitySvc := identity.New(pool)
	creatorSvc := creator.New(pool, creator.WithAgeGate(allowAll{}), creator.WithFakePayments(true))
	dm := messaging.New(pool)

	fan, fanPrincipal, err := account(ctx, identitySvc, fanEmail)
	if err != nil {
		return fmt.Errorf("seed fan: %w", err)
	}
	creatorUser, creatorPrincipal, err := account(ctx, identitySvc, creatorEmail)
	if err != nil {
		return fmt.Errorf("seed creator: %w", err)
	}

	if *expire {
		tag, err := pool.Exec(ctx, `
			UPDATE creator.entitlements
			   SET expires_at = now() - interval '1 minute'
			 WHERE fan_id = $1::uuid AND creator_id = $2::uuid`, fan, creatorUser)
		if err != nil {
			return fmt.Errorf("expire membership: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errors.New("expire membership: no entitlement found; seed first")
		}
		fmt.Println("membership ended for", fanEmail)
		return nil
	}

	// Profile and tier are created through the service so the same validation the
	// API applies is applied here.
	if _, err := creatorSvc.CreateProfile(ctx, creatorPrincipal, creator.ProfileInput{
		Handle:      creatorHandle,
		DisplayName: creatorName,
		Bio:         "Studio artist. New work every week.",
		Category:    "art",
		Publish:     ptr(true),
	}, "e2eseed-profile"); err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	price := int64(900)
	period := 30
	if _, err := creatorSvc.CreateTier(ctx, creatorPrincipal, creator.TierInput{
		Name:        "Studio",
		Description: "Every post and the monthly set.",
		PriceMinor:  &price,
		Currency:    "USD",
		PeriodDays:  &period,
		Perks:       []string{"All paid posts", "Direct messages"},
	}, "e2eseed-tier"); err != nil {
		return fmt.Errorf("create tier: %w", err)
	}

	// A live membership granted by support: source=admin, no subscription row, so
	// no money and no provider record are involved.
	if _, err := pool.Exec(ctx, `
		INSERT INTO creator.entitlements (fan_id, creator_id, scope, source, expires_at)
		VALUES ($1::uuid, $2::uuid, 'subscriber', 'admin', now() + interval '2 hours')`,
		fan, creatorUser); err != nil {
		return fmt.Errorf("grant membership: %w", err)
	}

	conv, err := dm.Open(ctx, fanPrincipal, creatorUser)
	if err != nil {
		return fmt.Errorf("open conversation: %w", err)
	}
	if _, err := dm.Send(ctx, creatorPrincipal, conv.ID,
		"Welcome in. The new set is up — tell me what you think."); err != nil {
		return fmt.Errorf("seed message: %w", err)
	}

	fixture := seed{
		BaseURL:        os.Getenv("E2E_BASE_URL"),
		FanEmail:       fanEmail,
		CreatorID:      creatorUser,
		CreatorName:    creatorName,
		ConversationID: conv.ID,
		FanID:          fan,
	}
	blob, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	if *out != "" {
		if err := os.WriteFile(*out, blob, 0o600); err != nil {
			return fmt.Errorf("write fixture: %w", err)
		}
	}
	return nil
}

// account registers the user, or signs in when it already exists, so a re-run
// against the same database is not an error.
func account(ctx context.Context, svc *identity.Service, email string) (string, identity.Principal, error) {
	res, err := svc.Register(ctx, email, password, identity.DeviceInfo{
		Fingerprint: "e2eseed", Platform: "ci", Name: "seed",
	}, "e2eseed-"+email)
	if err == nil {
		return res.User.ID, res.Principal, nil
	}
	if !errors.Is(err, identity.ErrEmailTaken) {
		return "", identity.Principal{}, err
	}
	res, err = svc.Login(ctx, email, password, "", identity.DeviceInfo{
		Fingerprint: "e2eseed", Platform: "ci", Name: "seed",
	}, "e2eseed-login-"+email)
	if err != nil {
		return "", identity.Principal{}, err
	}
	return res.User.ID, res.Principal, nil
}

func ptr[T any](v T) *T { return &v }
