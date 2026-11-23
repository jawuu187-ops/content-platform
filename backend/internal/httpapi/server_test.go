package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Env:             config.EnvLocal,
		ServiceName:     "api",
		RequestTimeout:  2 * time.Second,
		MaxRequestBytes: 1024,
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return New(Deps{
		Config: testConfig(),
		Logger: slog.New(slog.NewTextHandler(discard{}, nil)),
		Now:    func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestLivenessAndHeaders(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id response header")
	}
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("missing security header %s", h)
		}
	}
	body := decode[healthResponse](t, rec)
	if body.Status != "ok" || body.Service != "api" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestRequestIDIsReusedOnlyWhenSafe(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "abc-123-valid")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123-valid" {
		t.Fatalf("request id = %q, want the client value", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "bad id with spaces\ninjection")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got == "bad id with spaces\ninjection" || got == "" {
		t.Fatalf("unsafe request id was not replaced (got %q)", got)
	}
}

func TestReadinessWithoutDatabase(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"database":"not_configured"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestErrorShapeAndRouting(t *testing.T) {
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	p := decode[problem](t, rec)
	if p.Code != "not_found" || p.Status != http.StatusNotFound {
		t.Fatalf("unexpected problem: %+v", p)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestBodyLimitIsEnforced(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/meta", strings.NewReader(strings.Repeat("x", 4096)))
	req.ContentLength = 4096
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestMetaPublishesPolicy(t *testing.T) {
	cfg := testConfig()
	cfg.RequireAgeVerification = true
	h := New(Deps{Config: cfg, Logger: slog.New(slog.NewTextHandler(discard{}, nil))})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		MinimumAge              int  `json:"minimum_age"`
		AgeVerificationRequired bool `json:"age_verification_required"`
		ContentPolicy           struct {
			PublicSurface string `json:"public_surface"`
			PaidSurface   string `json:"paid_surface"`
		} `json:"content_policy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.MinimumAge != 18 || !body.AgeVerificationRequired {
		t.Fatalf("unexpected age policy: %+v", body)
	}
	if !strings.Contains(body.ContentPolicy.PublicSurface, "no nudity") {
		t.Fatalf("public surface policy missing: %+v", body.ContentPolicy)
	}
}

func TestPanicIsConvertedTo500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	h := chain(panicking, requestID, recoverPanic(logger))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatal("panic text leaked to the client")
	}
}
