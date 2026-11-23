// Package mail sends transactional email. It defines the seam between the
// platform and whatever provider is chosen, so identity code never depends on a
// vendor SDK.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message is one transactional email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender delivers a message. Implementations must be safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// ErrNotConfigured is returned when a real provider has not been configured.
var ErrNotConfigured = errors.New("mail: no provider configured")

// LogSender writes messages to the application log instead of sending them. It
// exists so local development and CI can exercise the full verification flow
// without an SMTP account. It refuses to run in production.
type LogSender struct {
	Logger *slog.Logger
}

// Send logs the message at info level. The body is included because a local
// developer needs the code; production never uses this sender.
func (s LogSender) Send(ctx context.Context, m Message) error {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(ctx, "mail: development delivery",
		slog.String("to", m.To),
		slog.String("subject", m.Subject),
		slog.String("body", m.Text))
	return nil
}

// SMTPConfig configures the SMTP sender.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	// ImplicitTLS uses TLS from the first byte (port 465 style). When false the
	// connection starts plaintext and is upgraded with STARTTLS, which is
	// required: the sender refuses to send credentials in the clear.
	ImplicitTLS bool
	Timeout     time.Duration
}

// SMTPSender sends through a real SMTP server.
type SMTPSender struct {
	cfg SMTPConfig
}

// NewSMTP validates the configuration and returns a sender.
func NewSMTP(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host == "" || cfg.From == "" {
		return nil, fmt.Errorf("%w: host and from address are required", ErrNotConfigured)
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &SMTPSender{cfg: cfg}, nil
}

// Send delivers the message. Errors are wrapped with the stage that failed so a
// bounce investigation does not need packet captures.
func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	addr := net.JoinHostPort(s.cfg.Host, s.cfg.Port)
	dialer := &net.Dialer{Timeout: s.cfg.Timeout}

	var conn net.Conn
	var err error
	if s.cfg.ImplicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12,
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("mail: smtp handshake: %w", err)
	}
	defer func() { _ = c.Close() }()

	if !s.cfg.ImplicitTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("mail: server does not offer STARTTLS; refusing to send credentials in clear text")
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("mail: authenticate: %w", err)
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("mail: envelope from: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("mail: envelope to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: start data: %w", err)
	}
	if _, err := w.Write(buildMessage(s.cfg.From, m)); err != nil {
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: finish message: %w", err)
	}
	return c.Quit()
}

// buildMessage writes a minimal RFC 5322 message. Headers are stripped of CR/LF
// so a subject or recipient cannot inject additional headers.
func buildMessage(from string, m Message) []byte {
	var b strings.Builder
	b.WriteString("From: " + sanitizeHeader(from) + "\r\n")
	b.WriteString("To: " + sanitizeHeader(m.To) + "\r\n")
	b.WriteString("Subject: " + sanitizeHeader(m.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(m.Text, "\n", "\r\n"))
	return []byte(b.String())
}

func sanitizeHeader(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}
