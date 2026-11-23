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
