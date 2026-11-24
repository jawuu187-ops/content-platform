package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/agegate"
	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/media"
	"github.com/jawuu187-ops/content-platform/backend/internal/payments"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/config"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/database"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// BuildInfo is injected at link time by the release workflow.
type BuildInfo struct {
	Version string
	Commit  string
	BuiltAt string
}

// Deps are the collaborators the HTTP surface needs. Optional dependencies are
// pointers so the service can boot in local mode without infrastructure and
// fail readiness instead of crashing.
type Deps struct {
	Config   *config.Config
	Logger   *slog.Logger
	Database *database.DB
	Identity *identity.Service
	// Creators is the creator profile, tier, subscription and access service.
	Creators *creator.Service
	// Payments is the payment-intent and ledger service.
	Payments *payments.Service
	// Media is the media ingestion and delivery service.
	Media *media.Service
	// AgeGate is the age-verification service. Nil answers 503 on its routes.
	AgeGate *agegate.Service
	// Limits is the shared rate-limit backend. Nil keeps the counters in process.
	Limits    *ratelimit.Backend
	Build     BuildInfo
	StartedAt time.Time
	Now       func() time.Time
}

// Server holds the resolved dependency set for handlers.
type Server struct {
	cfg       *config.Config
	logger    *slog.Logger
	db        *database.DB
	identity  *identity.Service
	creators  *creator.Service
	ageGate   *agegate.Service
	media     *media.Service
	payments  *payments.Service
	limits    *ratelimit.Backend
	build     BuildInfo
	startedAt time.Time
	now       func() time.Time
}

// New builds the fully wrapped HTTP handler for the public API.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.StartedAt.IsZero() {
		d.StartedAt = d.Now()
	}
	s := &Server{
		cfg:       d.Config,
		logger:    d.Logger,
		db:        d.Database,
		identity:  d.Identity,
		creators:  d.Creators,
		ageGate:   d.AgeGate,
		media:     d.Media,
		payments:  d.Payments,
		limits:    d.Limits,
		build:     d.Build,
		startedAt: d.StartedAt,
		now:       d.Now,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleLive)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /version", s.handleVersion)
	mux.HandleFunc("GET /v1/meta", s.handleMeta)

	// Identity is optional so the process can boot for operations endpoints
	// without a database. When it is absent the auth surface answers 503 rather
	// than 404, which tells a client the feature exists but the dependency is
	// down.
	if s.identity != nil {
		authHandler := newAuthHandler(s.identity, d.Logger, s.limits)
		authHandler.registerRoutes(mux)
		if s.payments != nil && s.creators != nil {
			newPaymentHandler(authHandler, s.payments, d.Logger, s.limits).registerRoutes(mux)
		} else {
			mux.HandleFunc("/v1/payments/", func(w http.ResponseWriter, r *http.Request) {
				writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
					"payments are not configured on this instance")
			})
			mux.HandleFunc("/v1/admin/ledger/", func(w http.ResponseWriter, r *http.Request) {
				writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
					"the ledger is not configured on this instance")
			})
		}
		if s.media != nil && s.creators != nil {
			newMediaHandler(authHandler, s.media, s.creators, d.Logger, s.limits).registerRoutes(mux)
		} else {
			mux.HandleFunc("/v1/media/", func(w http.ResponseWriter, r *http.Request) {
				writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
					"media delivery is not configured on this instance")
			})
		}
		if s.ageGate != nil {
			newAgeGateHandler(authHandler, s.ageGate, d.Logger, s.limits).registerRoutes(mux)
		} else {
			mux.HandleFunc("/v1/verify/", func(w http.ResponseWriter, r *http.Request) {
				writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
					"age verification is not configured on this instance")
			})
		}
		if s.creators != nil {
			newCreatorHandler(authHandler, s.creators, d.Logger, s.limits).registerRoutes(mux)
		} else {
			mux.HandleFunc("/v1/creators/", func(w http.ResponseWriter, r *http.Request) {
				writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
					"the creator service is not configured on this instance")
			})
		}
	} else {
		mux.HandleFunc("/v1/auth/", func(w http.ResponseWriter, r *http.Request) {
			writeProblem(w, r, http.StatusServiceUnavailable, "dependency_unavailable",
				"the identity service is not configured on this instance")
		})
	}
	// Anything else under a known method is a 404 with our error shape; other
	// methods produce 405 so clients can distinguish route from method errors.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			notFound(w, r)
			return
		}
		methodNotAllowed(w, r)
	})

	return chain(mux,
		requestID,
		recoverPanic(d.Logger),
		accessLog(d.Logger),
		securityHeaders,
		limitBody(d.Config.MaxRequestBytes),
		timeout(d.Config.RequestTimeout),
	)
}

// chain applies middleware so the first argument is the outermost layer.
func chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

type healthResponse struct {
	Status      string `json:"status"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Version     string `json:"version,omitempty"`
	Commit      string `json:"commit,omitempty"`
	UptimeSec   int64  `json:"uptime_seconds"`
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:      "ok",
		Service:     s.cfg.ServiceName,
		Environment: string(s.cfg.Env),
		Version:     s.build.Version,
		Commit:      s.build.Commit,
		UptimeSec:   int64(s.now().Sub(s.startedAt).Seconds()),
	})
}

// handleReady reports whether the process can serve traffic. It fails with 503
// when a required dependency is configured but unreachable, so orchestrators do
// not route to an instance that would error on every request.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	type checks struct {
		Database  string `json:"database"`
		RateLimit string `json:"rate_limit"`
	}
	out := healthResponse{
		Status:      "ready",
		Service:     s.cfg.ServiceName,
		Environment: string(s.cfg.Env),
		Version:     s.build.Version,
		UptimeSec:   int64(s.now().Sub(s.startedAt).Seconds()),
	}
	status := http.StatusOK
	dbStatus := "not_configured"
	limitStatus := "in_process"
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			dbStatus = "unreachable"
			out.Status = "degraded"
			status = http.StatusServiceUnavailable
		} else {
			dbStatus = "ok"
		}
	}
	// A configured shared limiter is a required dependency: if it is down, logins
	// fail closed, so the instance must be taken out of rotation rather than
	// answering "ready" while refusing every credential request.
	if s.limits != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.limits.Ping(ctx); err != nil {
			limitStatus = "unreachable"
			out.Status = "degraded"
			status = http.StatusServiceUnavailable
		} else {
			limitStatus = "ok"
		}
	}
	writeJSON(w, status, struct {
		healthResponse
		Checks checks `json:"checks"`
	}{out, checks{Database: dbStatus, RateLimit: limitStatus}})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Service string `json:"service"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
		BuiltAt string `json:"built_at"`
	}{s.cfg.ServiceName, s.build.Version, s.build.Commit, s.build.BuiltAt})
}

// handleMeta publishes the non-negotiable product rules so every client (web,
// Android, admin) reads them from one place instead of hard-coding policy.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Service                 string `json:"service"`
		MinimumAge              int    `json:"minimum_age"`
		AgeVerificationRequired bool   `json:"age_verification_required"`
		CryptoRailsEnabled      bool   `json:"crypto_rails_enabled"`
		Payments                struct {
			Available bool   `json:"available"`
			Provider  string `json:"provider"`
			// Simulated is true when the installed provider is the development one.
			// A client that sees it must label every amount it renders as test data.
			Simulated bool `json:"simulated"`
			// DataClass is "test" or "live", so an operator can assert the class of a
			// running instance without reading its environment.
			DataClass string `json:"data_class"`
		} `json:"payments"`
		ContentPolicy struct {
			PublicSurface string `json:"public_surface"`
			PaidSurface   string `json:"paid_surface"`
			AlwaysBlocked string `json:"always_blocked"`
		} `json:"content_policy"`
	}{
		Service:                 s.cfg.ServiceName,
		MinimumAge:              18,
		AgeVerificationRequired: s.cfg.RequireAgeVerification,
		CryptoRailsEnabled:      s.cfg.EnableCryptoRails,
		Payments: struct {
			Available bool   `json:"available"`
			Provider  string `json:"provider"`
			Simulated bool   `json:"simulated"`
			DataClass string `json:"data_class"`
		}{
			Available: s.payments != nil && s.payments.Configured(),
			Provider:  providerName(s.payments),
			Simulated: s.payments != nil && s.payments.Capabilities().Simulated,
			DataClass: dataClass(s.payments),
		},
		ContentPolicy: struct {
			PublicSurface string `json:"public_surface"`
			PaidSurface   string `json:"paid_surface"`
			AlwaysBlocked string `json:"always_blocked"`
		}{
			PublicSurface: "no nudity, no sexual suggestion",
			PaidSurface:   "non-explicit nudity and suggestive content only",
			AlwaysBlocked: "explicit/pornographic content, minors, non-consensual content",
		},
	})
}

// dataClass reports whether money this instance records would be test data or real.
func dataClass(svc *payments.Service) string {
	if svc != nil && svc.Capabilities().Simulated {
		return "test"
	}
	if svc != nil && svc.Configured() {
		return "live"
	}
	return "none"
}

// providerName reports the installed provider, or "unconfigured" when none is.
// Clients use it to decide whether checkout may be shown at all: an unknown or
// simulated provider must not be presented as a way to pay.
func providerName(svc *payments.Service) string {
	if svc == nil {
		return "unconfigured"
	}
	return svc.ProviderName()
}
