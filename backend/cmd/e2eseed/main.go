// Command e2eseed prepares the fixture a device test needs: one fan, one published
// creator with a paid tier and a live membership, a conversation the creator has
// already spoken in, and enough ledger history for the payout and tax-export
// surfaces to have something real to show.
//
// It exists so the emulator test exercises the real login, inbox, thread, send, tip,
// balance and statement paths instead of a fake server. It refuses to run anywhere
// except APP_ENV=demo or local with the fake payment provider, so it can never touch
// a staging or production database.
//
// The fixture includes ledger money in two data classes: one live settlement that a
// payout may spend and one simulated settlement that it may not. The live row is
// fixture money in a disposable demo database — no provider, no card, no bank — and
// is recorded as such in D-079. The membership itself is an "admin" grant of the kind
// support issues by hand, so no provider is involved there either.
//
// The fixture creation lives in seed.go so it can be asserted by a Go test; this file
// only reads the environment, opens the database and writes the fixture file.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
)

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
	password := os.Getenv("E2E_PASSWORD")
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

	opts := Options{BaseURL: os.Getenv("E2E_BASE_URL"), Password: password}

	if *expire {
		if err := ExpireMembership(ctx, db.Pool(), opts); err != nil {
			return err
		}
		fmt.Println("membership ended for", fanEmail)
		return nil
	}

	fixture, err := Seed(ctx, db.Pool(), opts)
	if err != nil {
		return err
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
