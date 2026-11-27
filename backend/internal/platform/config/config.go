// Package config loads runtime configuration from the environment.
//
// Configuration is validated at startup and the process refuses to start when
// a required value is missing or malformed. Secrets are never logged.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Environment identifies the deployment target.
type Environment string

const (
	EnvLocal Environment = "local"
	// EnvDemo is the internal demonstration environment: a real deployment with a
	// real database where the simulated payment provider is allowed and every
	// record it writes is stamped simulated. It is not staging and not production.
	EnvDemo       Environment = "demo"
	EnvStaging    Environment = "staging"
	EnvProduction Environment = "production"
)

// Config is the fully validated runtime configuration.
type Config struct {
	Env         Environment
	ServiceName string
	LogLevel    string
	LogFormat   string

	HTTPAddress      string
	PublicBaseURL    string
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
	RequestTimeout   time.Duration
	MaxRequestBytes  int64
	TrustedProxyCIDR []string

	DatabaseURL              string
	DatabaseMaxConns         int32
	DatabaseMinConns         int32
	DatabaseMaxConnTTL       time.Duration
	DatabaseStatementTimeout time.Duration

	ValkeyURL string

	// Mail delivery. SMTP is preferred when reachable; Brevo's HTTPS API is the
	// fallback for hosts that block outbound SMTP ports. Production refuses to
	// start without one of them.
	MailProvider    string
	MailFrom        string
	SMTPHost        string
	SMTPPort        string
	SMTPUsername    string
	SMTPPassword    string
	SMTPImplicitTLS bool
	BrevoAPIKey     string

	// MFAKeyBase64 encrypts TOTP secrets at rest (32 bytes, base64).
	MFAKeyBase64 string

	// Passkeys (WebAuthn). RPID is the registrable domain; origins must be a
	// comma-separated list of exact origins that are allowed to run ceremonies.
	WebAuthnRPID    string
	WebAuthnRPName  string
	WebAuthnOrigins []string

	// Secrets
	SessionSigningKey string
	TokenSigningKey   string
	WebhookSecretKey  string

	// Payments. "none" boots with the fail-closed provider; "fake" is a
	// development-only stand-in and is refused in production.
	PayProvider      string
	PayWebhookSecret string
	PayCommissionBPS int64

	// Media. Without a storage directory and a signing key, media endpoints answer
	// 503 and no content can be uploaded or delivered.
	MediaDir        string
	MediaSigningKey string
	MediaTokenTTL   time.Duration
	MediaImageBytes int64
	MediaVideoBytes int64
	MediaAudioBytes int64

	// Age verification. A provider callback is authenticated with an HMAC secret;
	// without one, verification cannot complete and 18+ content stays locked.
	AgeProvider       string
	AgeCallbackSecret string
	AgeMinAge         int
	AgeValidityDays   int

	// FakePayments activates subscriptions without a payment provider. It exists
	// for local development and CI; production refuses to start with it on.
	FakePayments bool

	// Owner activation. The secret is deployment-provided and bound to exactly
	// one pre-designated account; it is consumed once and never re-usable.
	OwnerActivationSecret string
	OwnerActivationUserID string
	OwnerActivationEmail  string

	// Feature gates that must be explicit in every environment.
	RequireAgeVerification bool
	EnableCryptoRails      bool
	MediaDeliveryBaseURL   string
}

// Load reads configuration from the environment and validates it.
func Load(serviceName string) (*Config, error) {
	env, err := parseEnv(get("APP_ENV", string(EnvLocal)))
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Env:              env,
		ServiceName:      serviceName,
		LogLevel:         get("LOG_LEVEL", defaultLogLevel(env)),
		LogFormat:        get("LOG_FORMAT", defaultLogFormat(env)),
		HTTPAddress:      get("HTTP_ADDRESS", ":8080"),
		PublicBaseURL:    strings.TrimRight(get("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		ReadTimeout:      getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:     getDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:      getDuration("HTTP_IDLE_TIMEOUT", 90*time.Second),
		ShutdownTimeout:  getDuration("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
		RequestTimeout:   getDuration("HTTP_REQUEST_TIMEOUT", 10*time.Second),
		MaxRequestBytes:  getInt64("HTTP_MAX_REQUEST_BYTES", 1<<20),
		TrustedProxyCIDR: splitAndTrim(get("TRUSTED_PROXY_CIDRS", "")),

		DatabaseURL:              get("DATABASE_URL", ""),
		DatabaseMaxConns:         getInt32("DATABASE_MAX_CONNS", 20, 1, 10000),
		DatabaseMinConns:         getInt32("DATABASE_MIN_CONNS", 2, 0, 10000),
		DatabaseMaxConnTTL:       getDuration("DATABASE_MAX_CONN_TTL", 30*time.Minute),
		DatabaseStatementTimeout: getDuration("DATABASE_STATEMENT_TIMEOUT", 10*time.Second),

		ValkeyURL: get("VALKEY_URL", ""),

		MailProvider:    strings.ToLower(strings.TrimSpace(get("MAIL_PROVIDER", ""))),
		MailFrom:        get("MAIL_FROM", "no-reply@localhost"),
		SMTPHost:        get("SMTP_HOST", ""),
		SMTPPort:        get("SMTP_PORT", "587"),
		SMTPUsername:    get("SMTP_USERNAME", ""),
		SMTPPassword:    get("SMTP_PASSWORD", ""),
		SMTPImplicitTLS: getBool("SMTP_IMPLICIT_TLS", false),
		BrevoAPIKey:     get("BREVO_API_KEY", ""),

		MFAKeyBase64:      get("MFA_KEY", ""),
		WebAuthnRPID:      get("WEBAUTHN_RP_ID", "localhost"),
		WebAuthnRPName:    get("WEBAUTHN_RP_NAME", "Content Platform"),
		WebAuthnOrigins:   splitList(get("WEBAUTHN_ORIGINS", "http://localhost")),
		SessionSigningKey: get("SESSION_SIGNING_KEY", ""),
		TokenSigningKey:   get("TOKEN_SIGNING_KEY", ""),
		WebhookSecretKey:  get("WEBHOOK_SECRET_KEY", ""),

		PayProvider:            get("PAY_PROVIDER", "none"),
		PayWebhookSecret:       get("PAY_WEBHOOK_SECRET", ""),
		PayCommissionBPS:       getInt64("PAY_COMMISSION_BPS", 2000),
		MediaDir:               get("MEDIA_LOCAL_DIR", ""),
		MediaSigningKey:        get("MEDIA_SIGNING_KEY", ""),
		MediaTokenTTL:          getDuration("MEDIA_TOKEN_TTL", 60*time.Second),
		MediaImageBytes:        getInt64("MEDIA_MAX_IMAGE_BYTES", 10<<20),
		MediaVideoBytes:        getInt64("MEDIA_MAX_VIDEO_BYTES", 200<<20),
		MediaAudioBytes:        getInt64("MEDIA_MAX_AUDIO_BYTES", 60<<20),
		AgeProvider:            get("AGE_PROVIDER", "generic"),
		AgeCallbackSecret:      get("AGE_CALLBACK_SECRET", ""),
		AgeMinAge:              int(getInt64("AGE_MIN_AGE", 18)),
		AgeValidityDays:        int(getInt64("AGE_VALIDITY_DAYS", 365)),
		FakePayments:           getBool("ENABLE_FAKE_PAYMENTS", false),
		OwnerActivationSecret:  get("OWNER_ACTIVATION_SECRET", ""),
		OwnerActivationUserID:  strings.TrimSpace(get("OWNER_ACTIVATION_USER_ID", "")),
		OwnerActivationEmail:   strings.ToLower(strings.TrimSpace(get("OWNER_ACTIVATION_EMAIL", ""))),
		RequireAgeVerification: getBool("REQUIRE_AGE_VERIFICATION", true),
		EnableCryptoRails:      getBool("ENABLE_CRYPTO_RAILS", false),
		MediaDeliveryBaseURL:   strings.TrimRight(get("MEDIA_DELIVERY_BASE_URL", ""), "/"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// IsProduction reports whether the process is running in production.
func (c *Config) IsProduction() bool { return c.Env == EnvProduction }

// IsLocal reports whether the process is running on a developer machine.
func (c *Config) IsLocal() bool { return c.Env == EnvLocal }

// IsDemo reports whether the process is the demo environment used for internal
// product demonstrations. It is a real environment with a real database, so it is
// kept separate from staging — but simulated payments are allowed there and
// nowhere else, and every row they create is stamped simulated=true.
func (c *Config) IsDemo() bool { return c.Env == EnvDemo }

// AllowsTestPayments reports whether simulated payments may run here: a developer
// machine or the demo environment. Production and staging never allow them, so a
// staging instance that looks like production cannot quietly settle fake access.
func (c *Config) AllowsTestPayments() bool { return c.IsLocal() || c.IsDemo() }

var errInvalid = errors.New("config: invalid configuration")

func (c *Config) validate() error {
	var problems []string

	if c.DatabaseURL == "" && !c.IsLocal() {
		problems = append(problems, "DATABASE_URL is required outside local development")
	}
	if c.MaxRequestBytes <= 0 {
		problems = append(problems, "HTTP_MAX_REQUEST_BYTES must be positive")
	}
	if c.DatabaseMinConns > c.DatabaseMaxConns {
		problems = append(problems, "DATABASE_MIN_CONNS must not exceed DATABASE_MAX_CONNS")
	}

	// Secret material is mandatory outside local development and must be long
	// enough to be meaningful. We deliberately never echo the value.
	for name, value := range map[string]string{
		"SESSION_SIGNING_KEY": c.SessionSigningKey,
		"TOKEN_SIGNING_KEY":   c.TokenSigningKey,
		"WEBHOOK_SECRET_KEY":  c.WebhookSecretKey,
	} {
		if c.IsLocal() {
			continue
		}
		if len(value) < 32 {
			problems = append(problems, fmt.Sprintf("%s must be set to at least 32 characters outside local", name))
		}
	}

	if !c.IsLocal() {
		switch c.MailProvider {
		case "", "auto":
			if c.SMTPHost == "" && c.BrevoAPIKey == "" {
				problems = append(problems, "a mail provider is required outside local development: set SMTP_HOST or BREVO_API_KEY (email verification cannot be delivered without one)")
			}
		case "smtp":
			if c.SMTPHost == "" {
				problems = append(problems, "SMTP_HOST is required when MAIL_PROVIDER=smtp")
			}
		case "brevo":
			if c.BrevoAPIKey == "" {
				problems = append(problems, "BREVO_API_KEY is required when MAIL_PROVIDER=brevo")
			}
		default:
			problems = append(problems, "MAIL_PROVIDER must be one of smtp, brevo or auto")
		}
		if c.MailFrom == "" || strings.HasSuffix(c.MailFrom, "@localhost") {
			problems = append(problems, "MAIL_FROM must be a real sender address outside local development")
		}
		if c.MFAKeyBase64 == "" {
			problems = append(problems, "MFA_KEY is required outside local development")
		}
		if c.WebAuthnRPID == "" || len(c.WebAuthnOrigins) == 0 {
			problems = append(problems, "WEBAUTHN_RP_ID and WEBAUTHN_ORIGINS are required outside local development")
		}
		for _, origin := range c.WebAuthnOrigins {
			if !strings.HasPrefix(origin, "https://") {
				problems = append(problems, fmt.Sprintf("WEBAUTHN_ORIGINS entry %q must use https outside local development", origin))
			}
		}
	}

	switch c.PayProvider {
	case "none":
	case "fake":
		if c.IsProduction() {
			problems = append(problems, "PAY_PROVIDER=fake is refused in production")
		}
		if len(c.PayWebhookSecret) < 16 {
			problems = append(problems, "PAY_WEBHOOK_SECRET must be at least 16 characters when a provider is configured")
		}
	default:
		problems = append(problems, "PAY_PROVIDER must be none or fake until a real gateway is contracted")
	}
	if c.PayCommissionBPS < 0 || c.PayCommissionBPS > 5000 {
		problems = append(problems, "PAY_COMMISSION_BPS must be between 0 and 5000")
	}

	if c.MediaSigningKey != "" && len(c.MediaSigningKey) < 32 {
		problems = append(problems, "MEDIA_SIGNING_KEY must be at least 32 characters")
	}
	if c.MediaTokenTTL < 5*time.Second || c.MediaTokenTTL > 15*time.Minute {
		problems = append(problems, "MEDIA_TOKEN_TTL must be between 5s and 15m")
	}
	if c.MediaImageBytes <= 0 || c.MediaVideoBytes <= 0 || c.MediaAudioBytes <= 0 || c.MediaImageBytes > c.MediaVideoBytes {
		problems = append(problems, "MEDIA_MAX_IMAGE_BYTES and MEDIA_MAX_VIDEO_BYTES must be positive, image not above video")
	}
	if c.IsProduction() && c.MediaDir != "" && c.MediaSigningKey == "" {
		problems = append(problems, "MEDIA_SIGNING_KEY is required when media storage is configured in production")
	}

	if c.AgeMinAge != 18 && c.AgeMinAge != 21 {
		problems = append(problems, "AGE_MIN_AGE must be 18 or 21")
	}
	if c.AgeValidityDays < 1 || c.AgeValidityDays > 3650 {
		problems = append(problems, "AGE_VALIDITY_DAYS must be between 1 and 3650")
	}
	if c.IsProduction() && c.AgeCallbackSecret == "" {
		problems = append(problems, "AGE_CALLBACK_SECRET is required in production: 18+ content must not be reachable without a working verification path")
	}
	if c.AgeCallbackSecret != "" && len(c.AgeCallbackSecret) < 16 {
		problems = append(problems, "AGE_CALLBACK_SECRET must be at least 16 characters")
	}

	// Simulated payments run on a developer machine and in the demo environment.
	// Staging and production refuse them, because an environment that looks like
	// production is exactly where a fake settlement gets mistaken for a real one.
	if c.FakePayments && !c.AllowsTestPayments() {
		problems = append(problems, "ENABLE_FAKE_PAYMENTS is allowed only with APP_ENV=local or APP_ENV=demo: it grants access without payment")
	}

	// A simulated provider is a development instrument. It must never be able to
	// settle access in production, even if the deployer asks for it by name.
	switch provider := strings.ToLower(strings.TrimSpace(c.PayProvider)); provider {
	case "", "none", "unavailable":
		// No provider: every payment route answers 503.
	case "fake":
		if !c.AllowsTestPayments() {
			problems = append(problems, "PAY_PROVIDER=fake is allowed only with APP_ENV=local or APP_ENV=demo: no simulated payment may settle real access")
		}
	default:
		problems = append(problems, "PAY_PROVIDER must be empty or fake until an approved gateway is integrated")
	}

	if c.IsProduction() && !c.RequireAgeVerification {
		problems = append(problems, "REQUIRE_AGE_VERIFICATION cannot be disabled in production")
	}

	if c.OwnerActivationSecret != "" {
		if len(c.OwnerActivationSecret) < 32 {
			problems = append(problems, "OWNER_ACTIVATION_SECRET must be at least 32 characters")
		}
		byID := c.OwnerActivationUserID != ""
		byEmail := c.OwnerActivationEmail != ""
		if byID == byEmail {
			problems = append(problems, "OWNER_ACTIVATION_SECRET requires exactly one of OWNER_ACTIVATION_USER_ID or OWNER_ACTIVATION_EMAIL")
		}
		if byID && !uuidPattern.MatchString(c.OwnerActivationUserID) {
			problems = append(problems, "OWNER_ACTIVATION_USER_ID must be a UUID")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", errInvalid, strings.Join(problems, "; "))
	}
	return nil
}

// uuidPattern accepts the canonical UUID shape used by identity user ids.
var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Redacted returns a copy of the configuration safe for logging.
func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"env":                      c.Env,
		"service_name":             c.ServiceName,
		"log_level":                c.LogLevel,
		"http_address":             c.HTTPAddress,
		"public_base_url":          c.PublicBaseURL,
		"require_age_verification": c.RequireAgeVerification,
		"fake_payments":            c.FakePayments,
		"age_provider":             c.AgeProvider,
		"age_min_age":              c.AgeMinAge,
		"media_configured":         c.MediaDir != "",
		"payment_provider":         c.PayProvider,
		"enable_crypto_rails":      c.EnableCryptoRails,
		"media_delivery_base_url":  c.MediaDeliveryBaseURL,
		"session_signing_key":      redact(c.SessionSigningKey),
		"token_signing_key":        redact(c.TokenSigningKey),
		"webhook_secret_key":       redact(c.WebhookSecretKey),
		"mfa_key":                  redact(c.MFAKeyBase64),
		"valkey_url":               redact(c.ValkeyURL),
		"webauthn_rp_id":           c.WebAuthnRPID,
		"webauthn_origins":         strings.Join(c.WebAuthnOrigins, ","),
		"mail_provider":            c.MailProvider,
		"smtp_host":                c.SMTPHost,
		"mail_from":                c.MailFrom,
	}
}

// splitList turns a comma-separated setting into a trimmed list, dropping empty
// entries so a trailing comma is not a configuration error.
func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func redact(v string) string {
	if v == "" {
		return "<unset>"
	}
	return fmt.Sprintf("<set:%d chars>", len(v))
}

func parseEnv(v string) (Environment, error) {
	switch Environment(strings.ToLower(v)) {
	case EnvLocal:
		return EnvLocal, nil
	case EnvDemo:
		return EnvDemo, nil
	case EnvStaging:
		return EnvStaging, nil
	case EnvProduction:
		return EnvProduction, nil
	default:
		return "", fmt.Errorf("%w: APP_ENV must be one of local, demo, staging, production (got %q)", errInvalid, v)
	}
}

func defaultLogLevel(e Environment) string {
	if e == EnvProduction {
		return "info"
	}
	return "debug"
}

func defaultLogFormat(e Environment) string {
	if e == EnvProduction {
		return "json"
	}
	return "text"
}

func get(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// getInt32 reads an integer, falls back when the value is missing or outside the
// accepted window, and only then narrows to int32. The window is a subset of the
// int32 range, so the conversion cannot overflow (the cast is marked for the
// static analyser with that invariant).
func getInt32(key string, fallback, min, max int64) int32 {
	v := getInt64(key, fallback)
	if v < min || v > max {
		return int32(fallback) // #nosec G115 -- fallback is a compile-time constant in range
	}
	return int32(v) // #nosec G115 -- v is clamped to [min,max] inside the int32 range above
}

func getInt64(key string, fallback int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func getBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitAndTrim(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
