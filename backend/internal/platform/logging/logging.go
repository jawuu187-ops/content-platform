// Package logging configures the process logger.
//
// Logs are structured (slog) and always carry the service name, environment and
// a request id when one is available. Helper functions exist so that the redaction
// rules for sensitive fields live in exactly one place.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey string

const (
	loggerKey  ctxKey = "logger"
	requestKey ctxKey = "request_id"
)

// Options configures the logger.
type Options struct {
	Service     string
	Env         string
	Level       string
	Format      string
	Version     string
	CommitSHA   string
}

// New builds the base logger for the process.
func New(opts Options) *slog.Logger {
	level := parseLevel(opts.Level)

	var handler slog.Handler
	base := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			return redactAttr(a)
		},
	}

	if strings.EqualFold(opts.Format, "json") {
		handler = slog.NewJSONHandler(os.Stdout, base)
	} else {
		handler = slog.NewTextHandler(os.Stdout, base)
	}

	logger := slog.New(handler).With(
		slog.String("service", opts.Service),
		slog.String("env", opts.Env),
	)
	if opts.Version != "" {
		logger = logger.With(slog.String("version", opts.Version))
	}
	if opts.CommitSHA != "" {
		logger = logger.With(slog.String("commit", opts.CommitSHA))
	}
	return logger
}

// WithLogger stores a logger in the context.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the request-scoped logger, falling back to the default.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithRequestID stores the request id in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey, id)
}

// RequestID returns the request id stored in the context, if any.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestKey).(string); ok {
		return v
	}
	return ""
}

func parseLevel(v string) slog.Level {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// sensitiveKeys are attribute keys whose values must never be logged.
var sensitiveKeys = map[string]struct{}{
	"password": {}, "passwd": {}, "secret": {}, "token": {}, "access_token": {},
	"refresh_token": {}, "authorization": {}, "cookie": {}, "set-cookie": {},
	"api_key": {}, "apikey": {}, "private_key": {}, "card_number": {}, "pan": {},
	"cvv": {}, "cvc": {}, "ssn": {}, "document_number": {}, "iban": {},
	"date_of_birth": {}, "dob": {},
}

func redactAttr(a slog.Attr) slog.Attr {
	if _, ok := sensitiveKeys[strings.ToLower(a.Key)]; ok {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}
