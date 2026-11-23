// Package ratelimit provides a shared token-bucket limiter so that the allowance
// is enforced once for a whole deployment, not once per replica.
//
// The bucket state lives in Redis/Valkey and every decision is a single atomic
// script, so two instances that receive a request at the same instant cannot
// both spend the last token.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrUnavailable is returned when the shared store cannot be reached. Callers
// must fail closed on it: a limiter that silently stops limiting under load is
// worse than a 503, because brute-force traffic is exactly the traffic that
// overloads the store.
var ErrUnavailable = errors.New("ratelimit: shared store unavailable")

// bucketScript is a GCRA-style token bucket in a single Lua script.
//
// KEYS[1] bucket key
// ARGV[1] now in unix milliseconds (from the caller's clock, so a test can move
//
//	time and every replica agrees on the value it was given)
//
// ARGV[2] capacity (tokens)
// ARGV[3] window in milliseconds (time to refill from empty to full)
//
// Returns {allowed (0|1), retry_after_ms}.
//
// The whole read-modify-write runs inside Redis, so it is atomic across any
// number of application instances.
const bucketScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local window = tonumber(ARGV[3])

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil or ts == nil then
  tokens = capacity
  ts = now
end

local elapsed = now - ts
if elapsed < 0 then
  -- A clock that jumped backwards must not mint tokens.
  elapsed = 0
end
if elapsed > 0 then
  tokens = math.min(capacity, tokens + (elapsed / window) * capacity)
  ts = now
end

local allowed = 0
local retry = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry = math.ceil((1 - tokens) * window / capacity)
end

redis.call('HSET', key, 'tokens', tostring(tokens), 'ts', tostring(ts))
-- The key expires once the bucket would be full again, so idle keys do not
-- accumulate.
redis.call('PEXPIRE', key, math.ceil(window))

if allowed == 1 then
  return {1, 0}
end
return {0, retry}
`

// Backend is a Redis/Valkey-backed limiter shared by every replica.
type Backend struct {
	client  *redis.Client
	prefix  string
	script  *redis.Script
	timeout time.Duration
}

// New connects to the shared store. The URL is a standard redis:// or rediss://
// DSN as used for Valkey.
func New(ctx context.Context, url, prefix string) (*Backend, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("ratelimit: parse url: %w", err)
	}
	opts.MaxRetries = 1
	opts.DialTimeout = 2 * time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ratelimit: ping: %w", err)
	}
	return &Backend{
		client:  client,
		prefix:  prefix,
		script:  redis.NewScript(bucketScript),
		timeout: 2 * time.Second,
	}, nil
}

// Allow spends one token for key. A nil error with allowed=false means the
// caller is over the limit and should be told how long to wait.
func (b *Backend) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if limit <= 0 || window <= 0 {
		return false, 0, fmt.Errorf("ratelimit: limit and window must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	now := time.Now().UnixMilli()
	res, err := b.script.Run(ctx, b.client, []string{b.prefix + key},
		now, limit, window.Milliseconds()).Slice()
	if err != nil {
		// A timeout or connection error is not "allowed": fail closed.
		return false, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if len(res) != 2 {
		return false, 0, fmt.Errorf("%w: unexpected script reply", ErrUnavailable)
	}
	allowed, ok := res[0].(int64)
	if !ok {
		return false, 0, fmt.Errorf("%w: unexpected script reply type", ErrUnavailable)
	}
	retryMs, _ := res[1].(int64)
	if allowed == 1 {
		return true, 0, nil
	}
	return false, time.Duration(retryMs) * time.Millisecond, nil
}

// Ping reports whether the shared store is reachable; the readiness probe uses
// it so a deployment with a broken limiter is taken out of rotation instead of
// failing every login.
func (b *Backend) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := b.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// Close releases the connection pool.
func (b *Backend) Close() error { return b.client.Close() }
