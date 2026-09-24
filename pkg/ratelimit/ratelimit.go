// Package ratelimit implements fixed-window rate limits backed by Redis
// (LLD v2.0 §8, API Spec §1.6) and HTTP middleware that applies them.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rule is a named limit: at most Limit requests per Window.
type Rule struct {
	Name   string
	Limit  int
	Window time.Duration
}

// Decision is the outcome of one Allow call.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration // time until the window resets
}

// Limiter counts requests per key.
type Limiter interface {
	Allow(ctx context.Context, rule Rule, key string) (Decision, error)
}

// fixedWindow increments the counter and, on the first hit of a window, sets
// its expiry, atomically. (INCR then EXPIRE as two calls can leave a key that
// never expires if the process dies in between.) Returns {count, ttl_ms}.
var fixedWindow = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

// Redis is a Limiter using one counter per (rule, key, window).
type Redis struct {
	Client *redis.Client
	Prefix string // default "rl"
}

// Allow implements Limiter.
func (l *Redis) Allow(ctx context.Context, rule Rule, key string) (Decision, error) {
	prefix := l.Prefix
	if prefix == "" {
		prefix = "rl"
	}
	res, err := fixedWindow.Run(ctx, l.Client, []string{prefix + ":" + rule.Name + ":" + key}, rule.Window.Milliseconds()).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: %w", err)
	}
	count, ttl := int(res[0]), time.Duration(res[1])*time.Millisecond
	return Decision{
		Allowed:    count <= rule.Limit,
		Limit:      rule.Limit,
		Remaining:  max(0, rule.Limit-count),
		RetryAfter: ttl,
	}, nil
}
