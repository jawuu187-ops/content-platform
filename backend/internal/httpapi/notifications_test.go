package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/notifications"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

func TestNotificationRoutesAreOwnerScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	ids := identity.New(pool, identity.WithHasher(identity.NewHasher(identity.TestParams())))
	svc := notifications.New(pool)
	handler := New(Deps{Config: testConfig(), Logger: loggerDiscard(), Identity: ids, Notifications: svc})

	me := registerAndGetToken(t, handler, "notify-me@example.test")
	other := registerAndGetToken(t, handler, "notify-other@example.test")
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO notifications.items (user_id, category, title, body)
		VALUES ($1::uuid, 'system', 'Hello', 'One item') RETURNING id::text`, me.User.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, handler, http.MethodGet, "/v1/notifications", me.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	body := decode[struct {
		Notifications []struct {
			ID string `json:"id"`
		} `json:"notifications"`
		Unread int `json:"unread_count"`
	}](t, rec)
	if len(body.Notifications) != 1 || body.Unread != 1 {
		t.Fatalf("body = %+v", body)
	}

	rec = doJSON(t, handler, http.MethodPost, "/v1/notifications/"+id+"/read", other.Tokens.AccessToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other read: want 404, got %d", rec.Code)
	}
	rec = doJSON(t, handler, http.MethodPost, "/v1/notifications/"+id+"/read", me.Tokens.AccessToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("own read: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodPatch, "/v1/notifications/preferences", me.Tokens.AccessToken,
		encodeBody(t, map[string]any{"engagement": false, "email": true, "push": true}))
	if rec.Code != http.StatusOK {
		t.Fatalf("prefs: %d %s", rec.Code, rec.Body.String())
	}
	prefs := decode[struct {
		Preferences notifications.Preferences `json:"preferences"`
	}](t, rec).Preferences
	if prefs.Engagement || prefs.Email || prefs.Push {
		t.Fatalf("prefs = %+v; email/push must stay off without providers", prefs)
	}
}
