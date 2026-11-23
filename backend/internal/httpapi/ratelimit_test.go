package httpapi

import (
	"strconv"
	"testing"
	"time"
)

func newTestLimiter(limit int, window time.Duration) (*limiter, *time.Time) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter(limit, window)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterAllowsBurstThenRefills(t *testing.T) {
	l, now := newTestLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if ok, _ := l.allow("client"); !ok {
			t.Fatalf("attempt %d within the limit was rejected", i+1)
		}
	}
	ok, retry := l.allow("client")
	if ok {
		t.Fatal("fourth attempt exceeded the limit but was allowed")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry-after out of range: %v", retry)
	}

	// A third of the window refills exactly one token at 3/minute.
	*now = now.Add(20 * time.Second)
	if ok, _ := l.allow("client"); !ok {
		t.Fatal("token was not refilled after a third of the window")
	}
	if ok, _ := l.allow("client"); ok {
		t.Fatal("refill handed out more than one token")
	}

	// A full window restores the burst allowance.
	*now = now.Add(2 * time.Minute)
	for i := 0; i < 3; i++ {
		if ok, _ := l.allow("client"); !ok {
			t.Fatalf("attempt %d after a full window was rejected", i+1)
		}
	}
}

func TestLimiterIsolatesKeys(t *testing.T) {
	l, _ := newTestLimiter(1, time.Minute)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("first key rejected")
	}
	if ok, _ := l.allow("a"); ok {
		t.Fatal("first key over its limit")
	}
	if ok, _ := l.allow("b"); !ok {
		t.Fatal("second key must have its own bucket")
	}
}

func TestLimiterSweepsStaleKeys(t *testing.T) {
	l, now := newTestLimiter(5, time.Minute)
	for i := 0; i < 10_001; i++ {
		l.allow("key-" + strconv.Itoa(i))
	}
	*now = now.Add(2 * time.Minute)
	l.allow("fresh")
	if len(l.buckets) > 100 {
		t.Fatalf("stale buckets were not swept: %d remain", len(l.buckets))
	}
}
