package agegate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Defaults chosen so an unconfigured deployment is safe rather than permissive.
const (
	defaultMinAge       = 18
	defaultValidity     = 365 * 24 * time.Hour
	defaultStartLimit   = 5
	defaultStartWindow  = 24 * time.Hour
	maxRevocationReason = 200
)

// Service owns the verification lifecycle. It is the only package that decides
// whether an account is age verified.
type Service struct {
	store    *store
	provider Provider
	logger   *slog.Logger
	now      func() time.Time
	minAge   int
	validity time.Duration
	starts   int
	window   time.Duration
}

// Option configures the service.
type Option func(*Service)

// WithProvider installs the verification provider. Without one every check
// fails closed with ErrUnavailable.
func WithProvider(p Provider) Option { return func(s *Service) { s.provider = p } }

// WithClock overrides the clock for deterministic tests.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithValidity sets how long a successful verification is trusted.
func WithValidity(d time.Duration) Option { return func(s *Service) { s.validity = d } }

// WithMinAge sets the age floor recorded on new checks.
func WithMinAge(age int) Option { return func(s *Service) { s.minAge = age } }

// WithStartLimit sets how many checks an account may start per window.
func WithStartLimit(n int, window time.Duration) Option {
	return func(s *Service) { s.starts, s.window = n, window }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.logger = l } }

// New builds the service.
func New(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{
		store:    &store{pool: pool},
		provider: NewUnavailable("no age verification provider is configured"),
		logger:   slog.Default(),
		now:      time.Now,
		minAge:   defaultMinAge,
		validity: defaultValidity,
		starts:   defaultStartLimit,
		window:   defaultStartWindow,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Provider exposes the configured provider for the operations endpoint.
func (s *Service) Provider() Provider { return s.provider }

// Status reports the account's effective verification state.
func (s *Service) Status(ctx context.Context, userID string) (State, error) {
	now := s.now().UTC()
	st := State{Status: StatusUnverified, MinAge: s.minAge, CanRetry: true, CheckedAt: now}

	check, err := s.store.liveVerified(ctx, userID)
	switch {
	case err == nil:
		if check.ExpiresAt != nil && !check.ExpiresAt.After(now) {
			st.Status = StatusExpired
			st.ExpiresAt = check.ExpiresAt
			return st, nil
		}
		st.Status = StatusVerified
		st.Verified = true
		st.ExpiresAt = check.ExpiresAt
		st.Assurance = check.Assurance
		st.CanRetry = false
		return st, nil
	case !errors.Is(err, ErrNoLiveCheck):
		return State{}, err
	}

	latest, err := s.store.latestCheck(ctx, userID)
	if errors.Is(err, ErrNoLiveCheck) {
		return st, nil
	}
	if err != nil {
		return State{}, err
	}
	switch latest.Status {
	case StatusPending:
		st.Status = StatusPending
		st.CanRetry = false
	case StatusFailed:
		st.Status = StatusFailed
	case StatusExpired:
		st.Status = StatusExpired
		st.ExpiresAt = latest.ExpiresAt
	case StatusRevoked:
		// A revocation is not self-service recoverable: support has to look at why.
		st.Status = StatusRevoked
		st.CanRetry = false
	}
	return st, nil
}

// AllowAdult is the Gate implementation used by the content layer. Any error
// other than a clean state read is treated as "not verified" by callers.
func (s *Service) AllowAdult(ctx context.Context, accountID string) (bool, error) {
	if accountID == "" {
		return false, nil
	}
	st, err := s.Status(ctx, accountID)
	if err != nil {
		return false, err
	}
	return st.Verified, nil
}

// Start opens a hosted verification check. It refuses to start when the
// provider cannot create one, which is the honest answer while no vendor is
// integrated.
func (s *Service) Start(ctx context.Context, userID string) (State, error) {
	if s.provider == nil || s.provider.Name() == "unconfigured" {
		return State{}, ErrUnavailable
	}
	if !s.provider.Capabilities().StartSupported {
		return State{}, ErrStartUnsupported
	}
	if userID == "" {
		return State{}, fmt.Errorf("%w: account is required", ErrInvalidInput)
	}
	current, err := s.Status(ctx, userID)
	if err != nil {
		return State{}, err
	}
	if current.Verified {
		return State{}, ErrAlreadyVerified
	}
	now := s.now().UTC()
	attempts, err := s.store.countStartsSince(ctx, userID, now.Add(-s.window))
	if err != nil {
		return State{}, err
	}
	if attempts >= s.starts {
		return State{}, ErrRateLimited
	}

	out, err := s.provider.Start(ctx, StartInput{AccountID: userID})
	if err != nil {
		_ = s.store.recordAttemptPool(ctx, userID, "rejected", "provider start failed")
		return State{}, err
	}
	check, err := s.store.insertCheck(ctx, userID, "provider", s.provider.Name(), out.Reference, "",
		s.minAge)
	if err != nil {
		return State{}, err
	}
	if err := s.store.recordAttemptPool(ctx, userID, "started", ""); err != nil {
		return State{}, err
	}
	return State{
		Status:    StatusPending,
		MinAge:    s.minAge,
		CanRetry:  false,
		ShowURL:   out.CheckURL,
		ExpiresAt: &out.ExpiresAt,
		CheckedAt: now,
	}, nil
}

// HandleCallback applies a provider answer. Signature verification happens in
// the provider, replay protection in the store.
func (s *Service) HandleCallback(ctx context.Context, headers http.Header, body []byte) (Check, error) {
	if s.provider == nil {
		return Check{}, ErrUnavailable
	}
	cb, err := s.provider.ParseCallback(headers, body, s.now().UTC())
	if err != nil {
		if errors.Is(err, ErrBadSignature) || errors.Is(err, ErrStaleCallback) {
			_ = s.store.recordAttemptPool(ctx, "", "rejected", "bad callback signature")
		}
		return Check{}, err
	}
	if cb.Status == StatusVerified && cb.ExpiresAt == nil {
		exp := s.now().UTC().Add(s.validity)
		cb.ExpiresAt = &exp
	}
	check, err := s.store.applyCallback(ctx, s.provider.Name(), cb, s.now().UTC())
	if errors.Is(err, ErrDuplicateCallback) {
		// Replays are not errors from the provider's point of view: the event has
		// already been applied and the state is correct.
		return Check{}, ErrDuplicateCallback
	}
	return check, err
}

// Revoke withdraws an account's verification and records who did it.
func (s *Service) Revoke(ctx context.Context, actorID, userID, reason string) (int64, error) {
	reason = strings.TrimSpace(reason)
	if userID == "" {
		return 0, fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	if reason == "" || len([]rune(reason)) > maxRevocationReason {
		return 0, fmt.Errorf("%w: a reason of 1-%d characters is required", ErrInvalidInput, maxRevocationReason)
	}
	return s.store.revoke(ctx, userID, actorID, reason, s.now().UTC())
}

// ExpireDue flips verifications whose validity window has ended.
func (s *Service) ExpireDue(ctx context.Context) (int64, error) {
	return s.store.expireDue(ctx, s.now().UTC())
}

// Capabilities describes the configured provider for operations.
func (s *Service) Capabilities() Capabilities {
	if s.provider == nil {
		return Capabilities{Name: "unconfigured"}
	}
	return s.provider.Capabilities()
}
