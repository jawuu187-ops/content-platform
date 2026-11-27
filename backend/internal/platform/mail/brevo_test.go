package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrevoSendsTransactionalMailOverHTTPS(t *testing.T) {
	var gotKey, gotPath string
	var got brevoPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"<test@brevo>"}`))
	}))
	defer srv.Close()

	sender, err := NewBrevo(BrevoConfig{
		APIKey: "test-key", From: "info@remixofficial.online",
		FromName: "Content Platform", Endpoint: srv.URL, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBrevo: %v", err)
	}
	if err := sender.Send(context.Background(), Message{
		To: "member@example.test", Subject: "Confirm your email", Text: "code 123456",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotKey != "test-key" || gotPath != "/" {
		t.Fatalf("unexpected request key=%q path=%q", gotKey, gotPath)
	}
	if got.Sender.Email != "info@remixofficial.online" || got.Sender.Name != "Content Platform" {
		t.Fatalf("unexpected sender: %+v", got.Sender)
	}
	if len(got.To) != 1 || got.To[0].Email != "member@example.test" {
		t.Fatalf("unexpected recipients: %+v", got.To)
	}
	if got.Subject != "Confirm your email" || !strings.Contains(got.TextContent, "123456") {
		t.Fatalf("unexpected content: %+v", got)
	}
}

func TestBrevoRejectsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Key not found"}`))
	}))
	defer srv.Close()

	sender, err := NewBrevo(BrevoConfig{
		APIKey: "bad", From: "info@remixofficial.online", Endpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewBrevo: %v", err)
	}
	err = sender.Send(context.Background(), Message{To: "member@example.test", Subject: "s", Text: "t"})
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected a rejection, got %v", err)
	}
}

func TestBrevoRequiresKeyAndSender(t *testing.T) {
	if _, err := NewBrevo(BrevoConfig{From: "info@remixofficial.online"}); err == nil {
		t.Fatal("missing API key must be rejected")
	}
	if _, err := NewBrevo(BrevoConfig{APIKey: "k"}); err == nil {
		t.Fatal("missing sender must be rejected")
	}
}
