package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jawuu187-ops/content-platform/backend/internal/agegate"
	"github.com/jawuu187-ops/content-platform/backend/internal/creator"
	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
	"github.com/jawuu187-ops/content-platform/backend/internal/media"
	"github.com/jawuu187-ops/content-platform/backend/internal/platform/dbtest"
)

const mediaTestKey = "media-signing-key-0123456789abcdef"

type mediaFixture struct {
	handler  http.Handler
	ids      *identity.Service
	provider *webhookTestProvider
	pool     *pgxpool.Pool
}

func newMediaFixture(t *testing.T) *mediaFixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ids := identity.New(pool,
		identity.WithHasher(identity.NewHasher(identity.TestParams())),
		identity.WithIssuer("Content Platform Test"))
	webhook, err := agegate.NewWebhookProvider("testco", []byte(ageTestSecret))
	if err != nil {
		t.Fatalf("webhook provider: %v", err)
	}
	provider := &webhookTestProvider{webhook: webhook, start: true}
	age := agegate.New(pool, agegate.WithProvider(provider), agegate.WithValidity(24*time.Hour))

	store, err := media.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	mediaSvc := media.New(pool,
		media.WithStorage(store),
		media.WithSigningKey([]byte(mediaTestKey)),
		media.WithTokenTTL(60*time.Second),
		media.WithLimits(media.Limits{ImageBytes: 4 << 20, VideoBytes: 8 << 20, MaxPixels: 20_000_000}),
	)
	creators := creator.New(pool, creator.WithFakePayments(true), creator.WithAgeGate(age))
	h := New(Deps{
		Config:   testConfig(),
		Logger:   loggerDiscard(),
		Identity: ids,
		Creators: creators,
		AgeGate:  age,
		Media:    mediaSvc,
	})
	return &mediaFixture{handler: h, ids: ids, provider: provider, pool: pool}
}

func pngFixture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func (f *mediaFixture) ageFixture() *ageFixture {
	return &ageFixture{handler: f.handler, provider: f.provider, ids: f.ids}
}

func uploadMedia(t *testing.T, h http.Handler, token string, body []byte) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/media", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "storage_key") || strings.Contains(rec.Body.String(), "sha256") {
		t.Fatalf("upload response leaked storage internals: %s", rec.Body.String())
	}
	var out struct {
		Media struct {
			ID string `json:"id"`
		} `json:"media"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if out.Media.ID == "" {
		t.Fatalf("upload returned no id: %s", rec.Body.String())
	}
	return out.Media.ID
}

func createPost(t *testing.T, h http.Handler, token string, body map[string]any) string {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/v1/posts", token, encodeBody(t, body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create post: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Post struct {
			ID string `json:"id"`
		} `json:"post"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode post: %v", err)
	}
	return out.Post.ID
}

func issueDelivery(t *testing.T, h http.Handler, token, mediaID, postID string) (int, string) {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/v1/media/"+mediaID+"/delivery", token,
		encodeBody(t, map[string]string{"post_id": postID}))
	if rec.Code != http.StatusCreated {
		return rec.Code, ""
	}
	var out struct {
		Delivery struct {
			URL string `json:"url"`
		} `json:"delivery"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode delivery: %v", err)
	}
	return rec.Code, out.Delivery.URL
}

func fetchDelivery(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

// newPost creates media + a published post and returns handle, tier id, post id, media id.
func newPost(t *testing.T, f *mediaFixture, visibility string, extra map[string]any) (authBody, string, string, string, string) {
	t.Helper()
	owner, pair := publishCreator(t, f.handler)
	handle, tierID := splitPair(pair)
	mediaID := uploadMedia(t, f.handler, owner.Tokens.AccessToken, pngFixture(t, 12, 9))
	body := map[string]any{"body": "hello", "visibility": visibility, "media_ids": []string{mediaID}, "published": true}
	for k, v := range extra {
		body[k] = v
	}
	postID := createPost(t, f.handler, owner.Tokens.AccessToken, body)
	return owner, handle, tierID, postID, mediaID
}

func subscribe(t *testing.T, f *mediaFixture, token, handle, tierID string) {
	t.Helper()
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/creators/"+handle+"/subscribe", token,
		encodeBody(t, map[string]any{"tier_id": tierID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
}

func makeAdmin(t *testing.T, ids *identity.Service, account authBody) {
	t.Helper()
	if _, err := ids.GrantRole(context.Background(), identity.Principal{UserID: account.User.ID, Roles: []string{identity.RoleAdmin}},
		account.User.ID, identity.RoleAdmin, ""); err != nil {
		t.Fatalf("grant admin: %v", err)
	}
}

func TestMediaUploadRejectsDisguisedFiles(t *testing.T) {
	f := newMediaFixture(t)
	owner, _ := publishCreator(t, f.handler)

	html := []byte("<html><body><script>alert(1)</script></body></html>")
	req := httptest.NewRequest(http.MethodPost, "/v1/media", bytes.NewReader(html))
	req.Header.Set("Authorization", "Bearer "+owner.Tokens.AccessToken)
	req.Header.Set("Content-Type", "image/png") // a claim the service ignores
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("html upload: want 415, got %d %s", rec.Code, rec.Body.String())
	}

	fan := registerAndGetToken(t, f.handler, "media-fan-"+randSuffix(t)+"@example.test")
	req = httptest.NewRequest(http.MethodPost, "/v1/media", bytes.NewReader(pngFixture(t, 4, 4)))
	req.Header.Set("Authorization", "Bearer "+fan.Tokens.AccessToken)
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("fan upload: want 403, got %d", rec.Code)
	}
}

func TestSubscriberMediaIsSingleUseAndRechecked(t *testing.T) {
	f := newMediaFixture(t)
	_, handle, tierID, postID, mediaID := newPost(t, f, creator.VisibilitySubscriber, nil)
	fan := registerAndGetToken(t, f.handler, "media-fan-"+randSuffix(t)+"@example.test")
	subscribe(t, f, fan.Tokens.AccessToken, handle, tierID)

	// Subscribed but not age-verified: no link may be issued.
	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID); code != http.StatusForbidden {
		t.Fatalf("unverified subscriber got a link: %d", code)
	}

	verifyAccount(t, f.ageFixture(), fan.Tokens.AccessToken, fan.User.ID, "evt_m_"+randSuffix(t),
		time.Now().Add(24*time.Hour))

	code, url := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID)
	if code != http.StatusCreated || url == "" {
		t.Fatalf("issue delivery: %d", code)
	}
	res := fetchDelivery(t, f.handler, url)
	if res.Code != http.StatusOK {
		t.Fatalf("redeem: %d %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type: %q", got)
	}
	if !strings.Contains(res.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("cache control: %q", res.Header().Get("Cache-Control"))
	}
	if res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
	if res.Body.Len() == 0 {
		t.Fatal("empty body")
	}

	if again := fetchDelivery(t, f.handler, url); again.Code != http.StatusForbidden {
		t.Fatalf("replay: want 403, got %d", again.Code)
	}
	tampered := url[:len(url)-1] + "0"
	if bad := fetchDelivery(t, f.handler, tampered); bad.Code != http.StatusForbidden {
		t.Fatalf("tampered token: want 403, got %d", bad.Code)
	}
}

// subscribedVerifiedFan returns a fan with a live subscription and a live
// verification, plus the subscriber-scoped post and media they may open.
func subscribedVerifiedFan(t *testing.T, f *mediaFixture) (authBody, string, string) {
	t.Helper()
	_, handle, tierID, postID, mediaID := newPost(t, f, creator.VisibilitySubscriber, nil)
	fan := registerAndGetToken(t, f.handler, "media-fan-"+randSuffix(t)+"@example.test")
	subscribe(t, f, fan.Tokens.AccessToken, handle, tierID)
	verifyAccount(t, f.ageFixture(), fan.Tokens.AccessToken, fan.User.ID, "evt_"+randSuffix(t),
		time.Now().Add(24*time.Hour))
	return fan, postID, mediaID
}

func TestLapsedSubscriptionClosesIssuedLink(t *testing.T) {
	f := newMediaFixture(t)
	fan, postID, mediaID := subscribedVerifiedFan(t, f)

	code, url := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID)
	if code != http.StatusCreated {
		t.Fatalf("issue: %d", code)
	}
	// A cancellation request keeps access until the period ends, so the boundary
	// that must close the link is the lapse itself: expire the entitlement the way
	// the scheduler would.
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE creator.entitlements SET expires_at = now() - interval '1 second'
		WHERE fan_id = $1::uuid`, fan.User.ID); err != nil {
		t.Fatalf("lapse entitlement: %v", err)
	}
	if res := fetchDelivery(t, f.handler, url); res.Code != http.StatusForbidden {
		t.Fatalf("link survived the subscription lapsing: %d", res.Code)
	}
	// And no new link may be minted.
	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID); code != http.StatusForbidden {
		t.Fatalf("new link after lapse: %d", code)
	}
}

func TestRevokedVerificationClosesIssuedLink(t *testing.T) {
	f := newMediaFixture(t)
	fan, postID, mediaID := subscribedVerifiedFan(t, f)

	code, url := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID)
	if code != http.StatusCreated {
		t.Fatalf("issue: %d", code)
	}
	admin := registerAndGetToken(t, f.handler, "media-admin-"+randSuffix(t)+"@example.test")
	makeAdmin(t, f.ids, admin)
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/admin/verify/age/"+fan.User.ID+"/revoke",
		admin.Tokens.AccessToken, encodeBody(t, map[string]any{"reason": "boundary test"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if res := fetchDelivery(t, f.handler, url); res.Code != http.StatusForbidden {
		t.Fatalf("link survived verification revocation: %d", res.Code)
	}
}

func TestPPVMediaNeedsAnUnlock(t *testing.T) {
	f := newMediaFixture(t)
	_, _, _, postID, mediaID := newPost(t, f, creator.VisibilityPPV,
		map[string]any{"price_minor": 500, "currency": "USD"})
	fan := registerAndGetToken(t, f.handler, "media-fan-"+randSuffix(t)+"@example.test")
	verifyAccount(t, f.ageFixture(), fan.Tokens.AccessToken, fan.User.ID, "evt_ppv_"+randSuffix(t),
		time.Now().Add(24*time.Hour))

	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID); code != http.StatusForbidden {
		t.Fatalf("ppv without unlock: want 403, got %d", code)
	}
	admin := registerAndGetToken(t, f.handler, "media-admin-"+randSuffix(t)+"@example.test")
	makeAdmin(t, f.ids, admin)
	rec := doJSON(t, f.handler, http.MethodPost, "/v1/posts/"+postID+"/unlocks", admin.Tokens.AccessToken,
		encodeBody(t, map[string]any{"fan_id": fan.User.ID}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("grant unlock: %d %s", rec.Code, rec.Body.String())
	}
	code, url := issueDelivery(t, f.handler, fan.Tokens.AccessToken, mediaID, postID)
	if code != http.StatusCreated {
		t.Fatalf("issue after unlock: %d", code)
	}
	if res := fetchDelivery(t, f.handler, url); res.Code != http.StatusOK {
		t.Fatalf("redeem after unlock: %d", res.Code)
	}
}

func TestPPVUnlockDoesNotOpenSubscriberPosts(t *testing.T) {
	f := newMediaFixture(t)
	_, subscriberHandle, tierID, subscriberPost, subscriberMedia := newPost(t, f, creator.VisibilitySubscriber, nil)
	_ = subscriberPost
	_, _, _, ppvPost, _ := newPost(t, f, creator.VisibilityPPV,
		map[string]any{"price_minor": 700, "currency": "USD"})
	fan := registerAndGetToken(t, f.handler, "media-fan-"+randSuffix(t)+"@example.test")
	verifyAccount(t, f.ageFixture(), fan.Tokens.AccessToken, fan.User.ID, "evt_scope_"+randSuffix(t),
		time.Now().Add(24*time.Hour))
	admin := registerAndGetToken(t, f.handler, "media-admin-"+randSuffix(t)+"@example.test")
	makeAdmin(t, f.ids, admin)
	if rec := doJSON(t, f.handler, http.MethodPost, "/v1/posts/"+ppvPost+"/unlocks", admin.Tokens.AccessToken,
		encodeBody(t, map[string]any{"fan_id": fan.User.ID})); rec.Code != http.StatusCreated {
		t.Fatalf("grant unlock: %d", rec.Code)
	}
	// Same account, different creator: the unlock must not travel.
	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, subscriberMedia, subscriberPost); code != http.StatusForbidden {
		t.Fatalf("ppv unlock opened another creator's post: %d", code)
	}
	// And a subscription on that creator still will not work without one.
	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, subscriberMedia, subscriberPost); code != http.StatusForbidden {
		t.Fatalf("expected gate: %d", code)
	}
	subscribe(t, f, fan.Tokens.AccessToken, subscriberHandle, tierID)
	if code, _ := issueDelivery(t, f.handler, fan.Tokens.AccessToken, subscriberMedia, subscriberPost); code != http.StatusCreated {
		t.Fatalf("subscribed fan refused: %d", code)
	}
}

func TestMediaCannotBePairedWithAnotherPost(t *testing.T) {
	f := newMediaFixture(t)
	owner, _, _, _, mediaID := newPost(t, f, creator.VisibilitySubscriber, nil)
	publicPost := createPost(t, f.handler, owner.Tokens.AccessToken, map[string]any{
		"body": "public", "visibility": creator.VisibilityPublic, "published": true,
	})
	if code, _ := issueDelivery(t, f.handler, owner.Tokens.AccessToken, mediaID, publicPost); code != http.StatusNotFound {
		t.Fatalf("foreign pairing: want 404, got %d", code)
	}
}

func TestConcurrentRedemptionOnlyOneWins(t *testing.T) {
	f := newMediaFixture(t)
	owner, _, _, postID, mediaID := newPost(t, f, creator.VisibilityPublic, nil)
	code, url := issueDelivery(t, f.handler, owner.Tokens.AccessToken, mediaID, postID)
	if code != http.StatusCreated {
		t.Fatalf("issue: %d", code)
	}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = fetchDelivery(t, f.handler, url).Code
		}(i)
	}
	close(start)
	wg.Wait()
	if !((codes[0] == http.StatusOK && codes[1] == http.StatusForbidden) ||
		(codes[0] == http.StatusForbidden && codes[1] == http.StatusOK)) {
		t.Fatalf("concurrent redemption result: %v", codes)
	}
}

func TestUnknownTokenIsRefused(t *testing.T) {
	f := newMediaFixture(t)
	bad := "/v1/media/delivery/00000000-0000-4000-8000-000000000000.deadbeef"
	if res := fetchDelivery(t, f.handler, bad); res.Code != http.StatusForbidden {
		t.Fatalf("unknown token: want 403, got %d", res.Code)
	}
}

func TestLockedFeedHidesNothingUsable(t *testing.T) {
	f := newMediaFixture(t)
	_, handle, _, _, _ := newPost(t, f, creator.VisibilitySubscriber, nil)
	rec := doJSON(t, f.handler, http.MethodGet, "/v1/creators/"+handle+"/posts", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("feed: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/v1/media/delivery/") {
		t.Fatalf("anonymous feed carried a delivery link: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"locked":true`) {
		t.Fatalf("anonymous feed did not mark the post locked: %s", rec.Body.String())
	}
}

func TestAuthenticatedMediaContentSupportsRangeAndRepeats(t *testing.T) {
	f := newMediaFixture(t)
	_, handle, tierID, postID, mediaID := newPost(t, f, creator.VisibilitySubscriber, nil)
	fan := registerAndGetToken(t, f.handler, "range-fan-"+randSuffix(t)+"@example.test")
	subscribe(t, f, fan.Tokens.AccessToken, handle, tierID)
	verifyAccount(t, f.ageFixture(), fan.Tokens.AccessToken, fan.User.ID, "evt_range_"+randSuffix(t),
		time.Now().Add(24*time.Hour))

	path := "/v1/posts/" + postID + "/media/" + mediaID + "/content"
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+fan.Tokens.AccessToken)
		req.Header.Set("Range", "bytes=0-3")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusPartialContent {
			t.Fatalf("range request %d: %d %s", i, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Range") == "" {
			t.Fatal("missing Content-Range")
		}
		if rec.Body.Len() != 4 {
			t.Fatalf("range bytes = %d, want 4", rec.Body.Len())
		}
	}
}
