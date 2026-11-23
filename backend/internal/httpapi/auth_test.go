package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

// authServer builds a handler with a real identity service backed by the test
// database. The HTTP layer is exercised end to end: routing, JSON decoding,
// bearer authentication and error mapping.
func authServer(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.Pool(t)
	svc := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	return New(Deps{
		Config:   testConfig(),
		Logger:   slog.New(slog.NewTextHandler(discard{}, nil)),
		Identity: svc,
	}), pool
}

func doJSON(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type authBody struct {
	User struct {
		ID            string   `json:"id"`
		Email         string   `json:"email"`
		EmailVerified bool     `json:"email_verified"`
		Roles         []string `json:"roles"`
	} `json:"user"`
	Tokens struct {
		AccessToken      string    `json:"access_token"`
		RefreshToken     string    `json:"refresh_token"`
		TokenType        string    `json:"token_type"`
		AccessExpiresAt  time.Time `json:"access_expires_at"`
		RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	} `json:"tokens"`
}

func decodeAuth(t *testing.T, rec *httptest.ResponseRecorder) authBody {
	t.Helper()
	var body authBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

type problemBody struct {
	Code   string `json:"code"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problemBody {
	t.Helper()
	var p problemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem %q: %v", rec.Body.String(), err)
	}
	return p
}

func registerBody(email, password string) string {
	b, _ := json.Marshal(map[string]any{
		"email":    email,
		"password": password,
		"device":   map[string]string{"fingerprint": "test-device", "platform": "android", "name": "Pixel"},
	})
	return string(b)
}

func TestAuthRegisterLoginRefreshLogoutFlow(t *testing.T) {
	h, _ := authServer(t)
	email := "flow@example.test"
	const password = "a-very-long-password"

	rec := doJSON(t, h, http.MethodPost, "/v1/auth/register", "", registerBody(email, password))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("credential response must be no-store, got %q", got)
	}
	registered := decodeAuth(t, rec)
	if registered.Tokens.AccessToken == "" || registered.Tokens.RefreshToken == "" {
		t.Fatal("register did not return tokens")
	}
	if registered.Tokens.TokenType != "Bearer" {
		t.Fatalf("want Bearer token type, got %q", registered.Tokens.TokenType)
	}
	if len(registered.User.Roles) != 1 || registered.User.Roles[0] != "fan" {
		t.Fatalf("want fan role, got %v", registered.User.Roles)
	}
	if !registered.Tokens.AccessExpiresAt.After(time.Now()) {
		t.Fatal("access token already expired")
	}

	// The password must not be echoed back in any form.
	if strings.Contains(rec.Body.String(), password) {
		t.Fatal("response contains the password")
	}

	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody(email, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	loggedIn := decodeAuth(t, rec)
	if loggedIn.User.ID != registered.User.ID {
		t.Fatal("login returned a different account")
	}

	rec = doJSON(t, h, http.MethodGet, "/v1/auth/me", loggedIn.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	reply, _ := json.Marshal(map[string]string{"refresh_token": loggedIn.Tokens.RefreshToken})
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/refresh", "", string(reply))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rotated := decodeAuth(t, rec)
	if rotated.Tokens.RefreshToken == loggedIn.Tokens.RefreshToken {
		t.Fatal("refresh did not rotate the refresh token")
	}

	// Replaying the consumed refresh token revokes the family.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/refresh", "", string(reply))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token replay: want 401, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "session_reuse" {
		t.Fatalf("token replay: want session_reuse, got %q", p.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/v1/auth/sessions", rotated.Tokens.AccessToken, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sessions after family revocation: want 401, got %d", rec.Code)
	}

	// A fresh login is still possible.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody(email, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login after revocation: want 200, got %d", rec.Code)
	}
	fresh := decodeAuth(t, rec)

	rec = doJSON(t, h, http.MethodPost, "/v1/auth/logout", fresh.Tokens.AccessToken, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/auth/me", fresh.Tokens.AccessToken, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d", rec.Code)
	}
}

func TestAuthErrorsAreSpecificAndDoNotEnumerate(t *testing.T) {
	h, _ := authServer(t)
	email := "errors@example.test"
	const password = "a-very-long-password"

	if rec := doJSON(t, h, http.MethodPost, "/v1/auth/register", "", registerBody(email, password)); rec.Code != http.StatusCreated {
		t.Fatalf("register: %d", rec.Code)
	}

	// Duplicate account.
	rec := doJSON(t, h, http.MethodPost, "/v1/auth/register", "", registerBody(email, password))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate register: want 409, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "email_taken" {
		t.Fatalf("duplicate register code: %q", p.Code)
	}

	// Weak password.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/register", "", registerBody("weak@example.test", "short"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password: want 400, got %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "weak_password" {
		t.Fatalf("weak password code: %q", p.Code)
	}

	// Malformed payload.
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", `{"email":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed json: want 400, got %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPost, "/v1/auth/login", "", `{"email":"a@b.test","password":"x","unexpected":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: want 400, got %d", rec.Code)
	}

	// Wrong password and unknown account produce the identical response.
	wrong := doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody(email, "wrong-password-here"))
	unknown := doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody("nobody@example.test", "wrong-password-here"))
	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for both, got %d and %d", wrong.Code, unknown.Code)
	}
	wp, up := decodeProblem(t, wrong), decodeProblem(t, unknown)
	if wp.Code != up.Code || wp.Detail != up.Detail || wp.Status != up.Status {
		t.Fatalf("responses differ and leak account existence: %+v vs %+v", wp, up)
	}

	// Missing and malformed bearer tokens.
	rec = doJSON(t, h, http.MethodGet, "/v1/auth/me", "", "")
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("missing token: want 401 with challenge, got %d", rec.Code)
	}
	rec = doJSON(t, h, http.MethodGet, "/v1/auth/me", "not-a-real-token", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: want 401, got %d", rec.Code)
	}
}

func TestAuthSessionListingAndRevocation(t *testing.T) {
	h, _ := authServer(t)
	email := "sessions@example.test"
	const password = "a-very-long-password"

	if rec := doJSON(t, h, http.MethodPost, "/v1/auth/register", "", registerBody(email, password)); rec.Code != http.StatusCreated {
		t.Fatalf("register: %d", rec.Code)
	}
	rec := doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody(email, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d", rec.Code)
	}
	current := decodeAuth(t, rec)

	rec = doJSON(t, h, http.MethodGet, "/v1/auth/sessions", current.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions: %d", rec.Code)
	}
	var list struct {
		Sessions []struct {
			ID      string  `json:"id"`
			Current bool    `json:"current"`
			Revoked *string `json:"revoked_at"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(list.Sessions))
	}

	var other string
	for _, s := range list.Sessions {
		if !s.Current {
			other = s.ID
		}
	}
	if other == "" {
		t.Fatal("no non-current session found")
	}

	rec = doJSON(t, h, http.MethodDelete, "/v1/auth/sessions/"+other, current.Tokens.AccessToken, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: want 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodDelete, "/v1/auth/sessions/"+other, current.Tokens.AccessToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double revoke: want 404, got %d", rec.Code)
	}
	// A session that belongs to nobody must not be revocable by an outsider.
	rec = doJSON(t, h, http.MethodDelete, "/v1/auth/sessions/00000000-0000-4000-8000-000000000000",
		current.Tokens.AccessToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign session id: want 404, got %d", rec.Code)
	}
}

func TestAuthLoginIsRateLimited(t *testing.T) {
	// A tight bucket keeps the test fast and deterministic; production uses ten
	// attempts per minute, which is exercised by the limiter's own unit tests.
	pool := dbtest.Pool(t)
	svc := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	ah := newAuthHandler(svc, slog.New(slog.NewTextHandler(discard{}, nil)))
	ah.login = newLimiter(3, time.Minute, nil)
	mux := http.NewServeMux()
	ah.registerRoutes(mux)
	h := http.Handler(mux)

	body := registerBody("ratelimit@example.test", "a-very-long-password")

	var limited *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		rec := doJSON(t, h, http.MethodPost, "/v1/auth/login", "", body)
		if rec.Code == http.StatusTooManyRequests {
			limited = rec
			break
		}
	}
	if limited == nil {
		t.Fatal("login was never rate limited after 6 attempts with a bucket of 3")
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("429 response is missing Retry-After")
	}
	if p := decodeProblem(t, limited); p.Code != "rate_limited" {
		t.Fatalf("want rate_limited, got %q", p.Code)
	}
}

func TestAuthEndpointsReportMissingDependency(t *testing.T) {
	h := newTestHandler(t) // no Identity wired
	rec := doJSON(t, h, http.MethodPost, "/v1/auth/login", "", registerBody("a@b.test", "a-very-long-password"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 without identity service, got %d", rec.Code)
	}
}
