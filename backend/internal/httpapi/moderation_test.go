package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reporting is part of the public surface, so its two failure shapes matter even when the
// service is not wired: an unauthenticated caller is rejected, and a deployment without the
// moderation service says so instead of 404 (which would read as "no such feature").

func TestReportingRequiresASession(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/reports",
		http.NoBody))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestModerationQueueIs503WithoutTheService(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/moderation/reports", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := decode[struct {
		Code string `json:"code"`
	}](t, rec)
	if body.Code != "dependency_unavailable" {
		t.Fatalf("code = %q, want dependency_unavailable", body.Code)
	}
}
