package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// These tests cover the passkey HTTP contract: which calls need a session, what
// a client gets back, and how ceremony failures are reported. The cryptographic
// verification itself is tested in the identity package with a software
// authenticator; duplicating that here would test the same code twice.

func passkeyServer(t *testing.T) http.Handler {
	t.Helper()
	return authServerWith(t, identity.WithWebAuthn(identity.WebAuthnConfig{
		RPID:                    "example.test",
		RPName:                  "Content Platform Test",
		Origins:                 []string{"https://app.example.test"},
		RequireUserVerification: true,
	}))
}

func TestPasskeyRoutesNeedPasskeyConfiguration(t *testing.T) {
	h := authServerWith(t) // no WithWebAuthn
	account := registerAndGetToken(t, h, "nopasskey@example.test")

	rec := doJSON(t, h, http.MethodPost, "/v1/auth/webauthn/register/start", account.Tokens.AccessToken, "{}")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("register start without configuration: want 503, got %d", rec.Code)
	}
}

func TestPasskeyRegisterStartRequiresAuthAndReturnsOptions(t *testing.T) {
	h := passkeyServer(t)

	rec := doJSON(t, h, http.MethodPost, "/v1/auth/webauthn/register/start", "", "{}")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous register start: want 401, got %d", rec.Code)
	}

	account := registerAndGetToken(t, h, "pk-register@example.test")
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/webauthn/register/start",
		account.Tokens.AccessToken, encodeBody(t, map[string]string{"label": "Pixel"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register start: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var opts struct {
		ChallengeID string `json:"challenge_id"`
		PublicKey   struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"rp"`
			User struct {
				Name string `json:"name"`
			} `json:"user"`
			AuthenticatorSelection struct {
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
		} `json:"public_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &opts); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	if opts.ChallengeID == "" || opts.PublicKey.Challenge == "" {
		t.Fatalf("options are incomplete: %s", rec.Body.String())
	}
	if opts.PublicKey.RP.ID != "example.test" || opts.PublicKey.User.Name != "pk-register@example.test" {
		t.Fatalf("options identify the wrong relying party or user: %s", rec.Body.String())
	}
	if opts.PublicKey.AuthenticatorSelection.UserVerification != "required" {
		t.Fatalf("user verification is not required: %s", rec.Body.String())
	}
}

func TestPasskeyLoginStartIsAnonymousAndFinishRejectsGarbage(t *testing.T) {
	h := passkeyServer(t)

	rec := doJSON(t, h, http.MethodPost, "/v1/auth/webauthn/login/start", "",
		encodeBody(t, map[string]string{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous login start: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var opts struct {
		ChallengeID string `json:"challenge_id"`
		PublicKey   struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"public_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &opts); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	if opts.PublicKey.RPID != "example.test" || opts.ChallengeID == "" {
		t.Fatalf("login options are wrong: %s", rec.Body.String())
	}

	// A structurally valid but cryptographically meaningless assertion must be
	// refused with a stable code, not a 500.
	body := encodeBody(t, map[string]any{
		"challenge_id": opts.ChallengeID,
		"credential": map[string]any{
			"id":                 "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"raw_id":             "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"client_data_json":   "eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0",
			"authenticator_data": "AAAA",
			"signature":          "AAAA",
		},
	})
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/webauthn/login/finish", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("garbage assertion: want 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if p := decodeProblem(t, rec); p.Code != "invalid_assertion" {
		t.Fatalf("garbage assertion problem code: %q", p.Code)
	}

	// A retry against the same challenge never succeeds: depending on whether the
	// credential was resolvable, the ceremony is either invalid or already over.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/webauthn/login/finish", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed challenge: want 401, got %d", rec.Code)
	}
	switch p := decodeProblem(t, rec); p.Code {
	case "challenge_expired", "invalid_assertion":
	default:
		t.Fatalf("replayed challenge problem code: %q", p.Code)
	}
}

func TestPasskeyCredentialsListAndRevokeNeedAuth(t *testing.T) {
	h := passkeyServer(t)
	account := registerAndGetToken(t, h, "pk-list@example.test")

	rec := doJSON(t, h, http.MethodGet, "/v1/auth/webauthn/credentials", account.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", rec.Code)
	}
	var list struct {
		Credentials []any `json:"credentials"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Credentials) != 0 {
		t.Fatalf("new account has credentials: %s", rec.Body.String())
	}

	// Revoking an id that is not theirs is a 404, never a 204.
	rec = doJSON(t, h, http.MethodDelete,
		"/v1/auth/webauthn/credentials/00000000-0000-4000-8000-000000000000",
		account.Tokens.AccessToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoke unknown: want 404, got %d", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/v1/auth/webauthn/credentials", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: want 401, got %d", rec.Code)
	}
}
