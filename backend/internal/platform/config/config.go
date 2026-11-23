// Package config loads runtime configuration from the environment.
//
// Configuration is validated at startup and the process refuses to start when
// a required value is missing or malformed. Secrets are never logged.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment identifies the deployment target.
type Environment string

const (
	EnvLocal      Environment = "local"
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

	// Secrets
	SessionSigningKey string
	TokenSigningKey   string
	WebhookSecretKey  string

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
		DatabaseMaxConns:         int32(getInt64("DATABASE_MAX_CONNS", 20)),
		DatabaseMinConns:         int32(getInt64("DATABASE_MIN_CONNS", 2)),
		DatabaseMaxConnTTL:       getDuration("DATABASE_MAX_CONN_TTL", 30*time.Minute),
		DatabaseStatementTimeout: getDuration("DATABASE_STATEMENT_TIMEOUT", 10*time.Second),

		ValkeyURL: get("VALKEY_URL", ""),

		SessionSigningKey: get("SESSION_SIGNING_KEY", ""),
		TokenSigningKey:   get("TOKEN_SIGNING_KEY", ""),
		WebhookSecretKey:  get("WEBHOOK_SECRET_KEY", ""),

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

	if c.IsProduction() && !c.RequireAgeVerification {
		problems = append(problems, "REQUIRE_AGE_VERIFICATION cannot be disabled in production")
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", errInvalid, strings.Join(problems, "; "))
	}
	return nil
}

// Redacted returns a copy of the configuration safe for logging.
func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"env":                      c.Env,
		"service_name":             c.ServiceName,
		"log_level":                c.LogLevel,
		"http_address":             c.HTTPAddress,
		"public_base_url":          c.PublicBaseURL,
		"require_age_verification": c.RequireAgeVerification,
		"enable_crypto_rails":      c.EnableCryptoRails,
		"media_delivery_base_url":  c.MediaDeliveryBaseURL,
		"session_signing_key":      redact(c.SessionSigningKey),
		"token_signing_key":        redact(c.TokenSigningKey),
		"webhook_secret_key":       redact(c.WebhookSecretKey),
	}
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
	case EnvStaging:
		return EnvStaging, nil
	case EnvProduction:
		return EnvProduction, nil
	default:
		return "", fmt.Errorf("%w: APP_ENV must be one of local, staging, production (got %q)", errInvalid, v)
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
