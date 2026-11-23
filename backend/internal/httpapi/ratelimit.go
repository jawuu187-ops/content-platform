package httpapi

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jawuu187-ops/content-platform/backend/internal/platform/ratelimit"
)

// limiter is an in-process token bucket keyed by an arbitrary string. It is the
// first line of defence for credential endpoints: it stops scripted password
// guessing and signup floods without a database round trip.
//
// When a shared backend is configured the bucket lives in Redis/Valkey and the
// allowance is enforced once for the whole deployment. Without one the counters
// are per process, which is correct for a single instance and N times the
// allowance behind N replicas; that mode is for local development only.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   float64
	window  time.Duration
	now     func() time.Time
	shared  *ratelimit.Backend
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(limit int, window time.Duration, shared *ratelimit.Backend) *limiter {
	return &limiter{
		buckets: make(map[string]*bucket),
		limit:   float64(limit),
		window:  window,
		now:     time.Now,
		shared:  shared,
	}
}

// allow consumes one token for key. When the bucket is empty it returns false and
// the caller should respond 429 with Retry-After.
func (l *limiter) allow(ctx context.Context, key string) (bool, time.Duration, error) {
	if l.shared != nil {
		return l.shared.Allow(ctx, key, int(l.limit), l.window)
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if len(l.buckets) > 10_000 {
		l.sweep(now)
	}

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.limit - 1, last: now}
		return true, 0, nil
	}

	refill := now.Sub(b.last).Seconds() * (l.limit / l.window.Seconds())
	if refill > 0 {
		b.tokens += refill
		if b.tokens > l.limit {
			b.tokens = l.limit
		}
		b.last = now
	}
	if b.tokens < 1 {
		needed := (1 - b.tokens) * (l.window.Seconds() / l.limit)
		return false, time.Duration(needed * float64(time.Second)), nil
	}
	b.tokens--
	return true, 0, nil
}

// sweep drops buckets that have fully refilled, so the map cannot grow without
// bound from one-off keys.
func (l *limiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.window {
			delete(l.buckets, k)
		}
	}
}

// clientKey identifies the caller for rate limiting. RemoteAddr is used rather
// than X-Forwarded-For: the header is client-controlled unless a trusted proxy
// is configured, and honouring it by default would let anyone bypass the limit.
func clientKey(r *http.Request, scope string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return scope + "|" + host
}

// rateLimited wraps a handler with a per-caller bucket.
func rateLimited(l *limiter, scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, retry, err := l.allow(r.Context(), clientKey(r, scope))
		if err != nil {
			// Fail closed and say so: this is a dependency failure, not a
			// limit breach, and the client should retry elsewhere.
			writeProblem(w, r, http.StatusServiceUnavailable, "rate_limit_unavailable",
				"the rate limiter is unreachable; the request was not processed")
			return
		}
		if !ok {
			seconds := int(retry.Seconds() + 0.999)
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeProblem(w, r, http.StatusTooManyRequests, "rate_limited",
				"too many attempts; wait before trying again")
			return
		}
		next(w, r)
	}
}
