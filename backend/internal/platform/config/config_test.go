package config

import (
	"strings"
	"testing"
	"time"
)

func base(t *testing.T) {
	t.Helper()
	for _, kv := range [][2]string{
		{"APP_ENV", "local"},
		{"DATABASE_URL", ""},
		{"HTTP_MAX_REQUEST_BYTES", "1048576"},
		{"SESSION_SIGNING_KEY", ""},
		{"TOKEN_SIGNING_KEY", ""},
		{"WEBHOOK_SECRET_KEY", ""},
		{"REQUIRE_AGE_VERIFICATION", "true"},
		{"DATABASE_MIN_CONNS", "2"},
		{"DATABASE_MAX_CONNS", "20"},
	} {
		t.Setenv(kv[0], kv[1])
	}
}

func TestLoadLocalWithoutDatabase(t *testing.T) {
	base(t)
	cfg, err := Load("api")
	if err != nil {
		t.Fatalf("local config should load without DATABASE_URL: %v", err)
	}
	if !cfg.IsLocal() || cfg.HTTPAddress != ":8080" {
		t.Fatalf("unexpected defaults: %+v", cfg.Redacted())
	}
	if !cfg.RequireAgeVerification {
		t.Fatal("age verification must default to required")
	}
}

func TestLoadRejectsMissingDatabaseOutsideLocal(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("SESSION_SIGNING_KEY", strings.Repeat("s", 40))
	t.Setenv("TOKEN_SIGNING_KEY", strings.Repeat("t", 40))
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("w", 40))
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL error, got %v", err)
	}
}

func TestLoadRejectsShortSecretsOutsideLocal(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("SESSION_SIGNING_KEY", "short")
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "SESSION_SIGNING_KEY") {
		t.Fatalf("expected session key error, got %v", err)
	}
}

func TestProductionCannotDisableAgeVerification(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("SESSION_SIGNING_KEY", strings.Repeat("s", 40))
	t.Setenv("TOKEN_SIGNING_KEY", strings.Repeat("t", 40))
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("w", 40))
	t.Setenv("REQUIRE_AGE_VERIFICATION", "false")
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "REQUIRE_AGE_VERIFICATION") {
		t.Fatalf("expected age verification error, got %v", err)
	}
}

func TestLoadRejectsInconsistentPoolSizes(t *testing.T) {
	base(t)
	t.Setenv("DATABASE_MIN_CONNS", "10")
	t.Setenv("DATABASE_MAX_CONNS", "5")
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_MIN_CONNS") {
		t.Fatalf("expected pool size error, got %v", err)
	}
}

func TestRedactedNeverExposesSecrets(t *testing.T) {
	cfg := &Config{
		SessionSigningKey: "super-secret-session-key-value",
		TokenSigningKey:   "super-secret-token-key-value",
		WebhookSecretKey:  "super-secret-webhook-key-value",
	}
	out := cfg.Redacted()
	for k, v := range out {
		if s, ok := v.(string); ok && strings.Contains(s, "super-secret") {
			t.Fatalf("redacted map leaks secret in %q", k)
		}
	}
}

func TestDurationParsingFallsBackOnGarbage(t *testing.T) {
	base(t)
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")
	cfg, err := Load("api")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReadTimeout != 15*time.Second {
		t.Fatalf("ReadTimeout = %v, want the default 15s", cfg.ReadTimeout)
	}
}

func TestPoolSizesOutsideTheAcceptedWindowFallBack(t *testing.T) {
	cases := map[string]struct {
		max, min         string
		wantMax, wantMin int32
	}{
		"huge":         {"99999999", "2", 20, 2},
		"zero max":     {"0", "0", 20, 0},
		"negative min": {"20", "-5", 20, 2},
		"valid custom": {"64", "8", 64, 8},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			base(t)
			t.Setenv("DATABASE_MAX_CONNS", tc.max)
			t.Setenv("DATABASE_MIN_CONNS", tc.min)
			cfg, err := Load("api")
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if cfg.DatabaseMaxConns != tc.wantMax || cfg.DatabaseMinConns != tc.wantMin {
				t.Fatalf("got max=%d min=%d, want max=%d min=%d",
					cfg.DatabaseMaxConns, cfg.DatabaseMinConns, tc.wantMax, tc.wantMin)
			}
		})
	}
}

func TestLoadRejectsFakePaymentsInProduction(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("SESSION_SIGNING_KEY", strings.Repeat("s", 40))
	t.Setenv("TOKEN_SIGNING_KEY", strings.Repeat("t", 40))
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("w", 40))
	t.Setenv("ENABLE_FAKE_PAYMENTS", "true")
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "ENABLE_FAKE_PAYMENTS") {
		t.Fatalf("expected ENABLE_FAKE_PAYMENTS refusal, got %v", err)
	}
}

func TestLoadRejectsFakeProviderInProduction(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("SESSION_SIGNING_KEY", strings.Repeat("s", 40))
	t.Setenv("TOKEN_SIGNING_KEY", strings.Repeat("t", 40))
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("w", 40))
	t.Setenv("PAY_PROVIDER", "fake")
	// A simulated provider may never be named outside local development.
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "PAY_PROVIDER=fake") {
		t.Fatalf("expected PAY_PROVIDER=fake refusal, got %v", err)
	}
}

// The simulated provider is the development and demonstration engine, so it must
// work in local and in demo. Those are the only two places it may ever work.
func TestLoadAllowsFakeProviderInLocalAndDemo(t *testing.T) {
	for _, env := range []string{"local", "demo"} {
		t.Run(env, func(t *testing.T) {
			base(t)
			t.Setenv("APP_ENV", env)
			if env != "local" {
				deployedEnv(t)
			}
			t.Setenv("PAY_PROVIDER", "fake")
			t.Setenv("PAY_WEBHOOK_SECRET", strings.Repeat("p", 32))
			cfg, err := Load("api")
			if err != nil {
				t.Fatalf("fake provider should be allowed in %s: %v", env, err)
			}
			if cfg.PayProvider != "fake" {
				t.Fatalf("provider = %q", cfg.PayProvider)
			}
			if !cfg.AllowsTestPayments() {
				t.Fatalf("%s must allow test payments", env)
			}
		})
	}
}

// Staging mirrors production, so it must refuse both the switch and the provider.
// deployedEnv fills the settings any non-local environment requires. Demo is a real
// deployment, so it needs them too; tests must not pretend otherwise.
func deployedEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("SESSION_SIGNING_KEY", strings.Repeat("s", 40))
	t.Setenv("TOKEN_SIGNING_KEY", strings.Repeat("t", 40))
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("w", 40))
	t.Setenv("SMTP_HOST", "smtp.example.test")
	t.Setenv("MAIL_FROM", "noreply@example.test")
	t.Setenv("MFA_KEY", strings.Repeat("m", 32))
	t.Setenv("WEBAUTHN_ORIGINS", "https://app.example.test")
}

func TestLoadRejectsSimulatedPaymentsInStaging(t *testing.T) {
	for _, tc := range []struct {
		name, env, value, want string
	}{
		{"switch", "ENABLE_FAKE_PAYMENTS", "true", "ENABLE_FAKE_PAYMENTS"},
		{"provider", "PAY_PROVIDER", "fake", "PAY_PROVIDER=fake"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base(t)
			t.Setenv("APP_ENV", "staging")
			t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
			t.Setenv("SESSION_SIGNING_KEY", strings.Repeat("s", 40))
			t.Setenv("TOKEN_SIGNING_KEY", strings.Repeat("t", 40))
			t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("w", 40))
			t.Setenv("PAY_WEBHOOK_SECRET", strings.Repeat("p", 32))
			t.Setenv(tc.env, tc.value)
			_, err := Load("api")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected staging to refuse %s, got %v", tc.env, err)
			}
		})
	}
}

func TestParseEnvAcceptsDemo(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "demo")
	deployedEnv(t)
	cfg, err := Load("api")
	if err != nil {
		t.Fatalf("demo should load: %v", err)
	}
	if !cfg.IsDemo() || cfg.IsProduction() || cfg.IsLocal() {
		t.Fatalf("demo environment misclassified: %+v", cfg.Env)
	}
}

func TestLoadRejectsUnapprovedProvider(t *testing.T) {
	base(t)
	t.Setenv("PAY_PROVIDER", "stripe")
	_, err := Load("api")
	if err == nil || !strings.Contains(err.Error(), "PAY_PROVIDER") {
		t.Fatalf("expected provider refusal, got %v", err)
	}
}
