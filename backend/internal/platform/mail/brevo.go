package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BrevoConfig configures the Brevo transactional email HTTPS API.
type BrevoConfig struct {
	APIKey   string
	From     string
	FromName string
	// Endpoint defaults to the public Brevo API. Tests replace it.
	Endpoint string
	Timeout  time.Duration
}

// BrevoSender sends transactional email over HTTPS. It exists because some hosts
// block outbound SMTP ports entirely; HTTPS is the only viable delivery path.
type BrevoSender struct {
	cfg    BrevoConfig
	client *http.Client
}

// NewBrevo validates the configuration and returns a sender.
func NewBrevo(cfg BrevoConfig) (*BrevoSender, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("%w: brevo api key is required", ErrNotConfigured)
	}
	if strings.TrimSpace(cfg.From) == "" {
		return nil, fmt.Errorf("%w: brevo sender address is required", ErrNotConfigured)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.brevo.com/v3/smtp/email"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &BrevoSender{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}, nil
}

type brevoPayload struct {
	Sender      brevoAddress   `json:"sender"`
	To          []brevoAddress `json:"to"`
	Subject     string         `json:"subject"`
	TextContent string         `json:"textContent"`
}

type brevoAddress struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// Send delivers one message. A non-2xx answer is an error; the body is bounded
// because provider error pages are not useful beyond a few hundred bytes.
func (s *BrevoSender) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(brevoPayload{
		Sender:      brevoAddress{Name: s.cfg.FromName, Email: s.cfg.From},
		To:          []brevoAddress{{Email: m.To}},
		Subject:     m.Subject,
		TextContent: m.Text,
	})
	if err != nil {
		return fmt.Errorf("mail: encode brevo request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mail: build brevo request: %w", err)
	}
	req.Header.Set("api-key", s.cfg.APIKey)
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: brevo request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("mail: brevo rejected the message with status %d: %s",
		resp.StatusCode, strings.TrimSpace(string(detail)))
}
