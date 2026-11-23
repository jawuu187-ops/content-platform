package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// The in-process limiter is tested with a fake clock; the shared limiter needs a
// real Redis, which CI provides. Both are exercised so a regression in either
// path is caught.

func newTestLimiter(limit int, window time.Duration) (*limiter, *time.Time) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter(limit, window, nil)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterAllowsBurstThenRefills(t *testing.T) {
	l, now := newTestLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if ok, _, err := l.allow(context.Background(), "client"); err != nil || !ok {
			t.Fatalf("attempt %d within the limit was rejected: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, retry, err := l.allow(context.Background(), "client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("fourth attempt exceeded the limit but was allowed")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry-after out of range: %v", retry)
	}

	// A third of the window refills exactly one token at 3/minute.
	*now = now.Add(20 * time.Second)
	if ok, _, _ := l.allow(context.Background(), "client"); !ok {
		t.Fatal("token was not refilled after a third of the window")
	}
	if ok, _, _ := l.allow(context.Background(), "client"); ok {
		t.Fatal("refill handed out more than one token")
	}

	// A full window restores the burst allowance.
	*now = now.Add(2 * time.Minute)
	for i := 0; i < 3; i++ {
		if ok, _, _ := l.allow(context.Background(), "client"); !ok {
			t.Fatalf("attempt %d after a full window was rejected", i+1)
		}
	}
}

func TestLimiterIsolatesKeys(t *testing.T) {
	l, _ := newTestLimiter(1, time.Minute)
	ctx := context.Background()
	if ok, _, _ := l.allow(ctx, "a"); !ok {
		t.Fatal("first key rejected")
	}
	if ok, _, _ := l.allow(ctx, "a"); ok {
		t.Fatal("first key over its limit")
	}
	if ok, _, _ := l.allow(ctx, "b"); !ok {
		t.Fatal("second key must have its own bucket")
	}
}

func TestLimiterSweepDropsIdleKeys(t *testing.T) {
	l, now := newTestLimiter(1, time.Minute)
	ctx := context.Background()
	for i := 0; i < 10_050; i++ {
		l.allow(ctx, "k"+time.Duration(i).String())
	}
	before := len(l.buckets)
	*now = now.Add(10 * time.Minute)
	l.sweep(*now)
	if len(l.buckets) >= before {
		t.Fatalf("sweep did not drop idle buckets: %d -> %d", before, len(l.buckets))
	}
}

// sharedBackend connects to the Redis that CI runs as a service. Locally the
// test skips: a missing service must not turn into a fake pass.
func sharedBackend(t *testing.T) *ratelimit.Backend {
	t.Helper()
	url := os.Getenv("VALKEY_URL")
	if url == "" {
		t.Skip("VALKEY_URL is not set; the shared limiter integration test needs a Redis service")
	}
	b, err := ratelimit.New(context.Background(), url, "test:rl:"+t.Name()+":")
	if err != nil {
		t.Fatalf("connect shared limiter: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestSharedLimiterIsConsistentAcrossInstances(t *testing.T) {
	url := os.Getenv("VALKEY_URL")
	if url == "" {
		t.Skip("VALKEY_URL is not set; the shared limiter integration test needs a Redis service")
	}
	ctx := context.Background()

	// Two independent instances, as two replicas would be: separate connection
	// pools with no shared memory.
	a, err := ratelimit.New(ctx, url, "test:rl:multi:")
	if err != nil {
		t.Fatalf("instance a: %v", err)
	}
	defer a.Close()
	b, err := ratelimit.New(ctx, url, "test:rl:multi:")
	if err != nil {
		t.Fatalf("instance b: %v", err)
	}
	defer b.Close()

	// A unique key per run keeps repeated runs independent.
	key := "multi:" + time.Now().Format(time.RFC3339Nano)
	const limit = 10
	const attempts = 40

	var allowed int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			backend := a
			if i%2 == 1 {
				backend = b
			}
			ok, _, err := backend.Allow(ctx, key, limit, time.Minute)
			if err != nil {
				t.Errorf("allow: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&allowed, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if allowed != limit {
		t.Fatalf("two instances sharing one budget allowed %d of %d attempts; want exactly %d",
			allowed, attempts, limit)
	}
}

func TestSharedLimiterExpiryRestoresTheBudget(t *testing.T) {
	b := sharedBackend(t)
	ctx := context.Background()
	key := "expiry:" + time.Now().Format(time.RFC3339Nano)
	window := 1500 * time.Millisecond

	if ok, _, err := b.Allow(ctx, key, 1, window); err != nil || !ok {
		t.Fatalf("first request: ok=%v err=%v", ok, err)
	}
	ok, retry, err := b.Allow(ctx, key, 1, window)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if ok {
		t.Fatal("second request inside the window was allowed")
	}
	if retry <= 0 || retry > window {
		t.Fatalf("retry-after %v is outside the window %v", retry, window)
	}

	time.Sleep(window + 150*time.Millisecond)
	if ok, _, err := b.Allow(ctx, key, 1, window); err != nil || !ok {
		t.Fatalf("request after the window: ok=%v err=%v", ok, err)
	}
}

func TestSharedLimiterBoundary(t *testing.T) {
	b := sharedBackend(t)
	ctx := context.Background()
	key := "boundary:" + time.Now().Format(time.RFC3339Nano)

	// Capacity 1: exactly one in flight, the second is refused with a retry hint
	// no larger than the window.
	if ok, _, err := b.Allow(ctx, key, 1, time.Minute); err != nil || !ok {
		t.Fatalf("first: ok=%v err=%v", ok, err)
	}
	ok, retry, err := b.Allow(ctx, key, 1, time.Minute)
	if err != nil || ok {
		t.Fatalf("second at capacity: ok=%v err=%v", ok, err)
	}
	if retry > time.Minute {
		t.Fatalf("retry-after %v exceeds the window", retry)
	}

	// Capacity 3 accepts a burst of three and refuses the fourth.
	burst := "burst:" + time.Now().Format(time.RFC3339Nano)
	for i := 0; i < 3; i++ {
		if ok, _, err := b.Allow(ctx, burst, 3, time.Minute); err != nil || !ok {
			t.Fatalf("burst attempt %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	if ok, _, err := b.Allow(ctx, burst, 3, time.Minute); err != nil || ok {
		t.Fatalf("fourth burst attempt: ok=%v err=%v", ok, err)
	}
}

func TestSharedLimiterFailsClosedWhenTheStoreIsGone(t *testing.T) {
	b := sharedBackend(t)
	ctx := context.Background()
	key := "down:" + time.Now().Format(time.RFC3339Nano)
	if ok, _, err := b.Allow(ctx, key, 5, time.Minute); err != nil || !ok {
		t.Fatalf("healthy store: ok=%v err=%v", ok, err)
	}

	// Closing the pool is the same failure a dead Redis produces: no reply.
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ok, _, err := b.Allow(ctx, key+"-2", 5, time.Minute)
	if err == nil {
		t.Fatal("a closed store returned no error; the limiter would have allowed traffic")
	}
	if ok {
		t.Fatal("a closed store allowed the request; the limiter must fail closed")
	}
	if !errors.Is(err, ratelimit.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestRateLimitedAnswers503WhenTheLimiterIsDown(t *testing.T) {
	b := sharedBackend(t)
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	l := newLimiter(10, time.Minute, b)
	called := false
	h := rateLimited(l, "login", func(w http.ResponseWriter, r *http.Request) { called = true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	h(rec, req)

	if called {
		t.Fatal("the wrapped handler ran even though the limiter failed")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "rate_limit_unavailable") {
		t.Fatalf("response does not name the failure: %s", rec.Body.String())
	}
}
