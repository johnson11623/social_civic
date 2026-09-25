package ratelimit

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Runs against a real Redis when TEST_REDIS_URL is set (`make test-integration`).
func testRedis(t *testing.T) *Redis {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set; run `make test-integration`")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opts)
	t.Cleanup(func() { c.Close() })
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return &Redis{Client: c, Prefix: "rltest:" + uuid.NewString()}
}

func TestRedisFixedWindow(t *testing.T) {
	l := testRedis(t)
	ctx := context.Background()
	rule := Rule{Name: "register", Limit: 5, Window: time.Hour}

	for i := 1; i <= 5; i++ {
		d, err := l.Allow(ctx, rule, "ip-a")
		if err != nil || !d.Allowed || d.Remaining != 5-i {
			t.Fatalf("request %d: %+v, %v", i, d, err)
		}
	}
	d, err := l.Allow(ctx, rule, "ip-a")
	if err != nil || d.Allowed || d.Remaining != 0 {
		t.Fatalf("6th request: %+v, %v", d, err)
	}
	if d.RetryAfter <= 59*time.Minute || d.RetryAfter > time.Hour {
		t.Errorf("RetryAfter = %s, want just under 1h", d.RetryAfter)
	}

	// The counter expires with the window (backlog: "Redis key expires after 1 hour").
	ttl, err := l.Client.PTTL(ctx, l.Prefix+":register:ip-a").Result()
	if err != nil || ttl <= 59*time.Minute || ttl > time.Hour {
		t.Errorf("key TTL = %s, %v", ttl, err)
	}

	if d, _ := l.Allow(ctx, rule, "ip-b"); !d.Allowed {
		t.Error("other keys must have their own counters")
	}
	if d, _ := l.Allow(ctx, Rule{Name: "login", Limit: 5, Window: time.Hour}, "ip-a"); !d.Allowed {
		t.Error("other rules must have their own counters")
	}
}

func TestRedisWindowResets(t *testing.T) {
	l := testRedis(t)
	ctx := context.Background()
	rule := Rule{Name: "short", Limit: 1, Window: 300 * time.Millisecond}
	if d, _ := l.Allow(ctx, rule, "k"); !d.Allowed {
		t.Fatal("first request denied")
	}
	if d, _ := l.Allow(ctx, rule, "k"); d.Allowed {
		t.Fatal("second request allowed within the window")
	}
	time.Sleep(400 * time.Millisecond)
	if d, _ := l.Allow(ctx, rule, "k"); !d.Allowed {
		t.Error("request denied after the window reset")
	}
}

func TestRedisIsAtomicUnderConcurrency(t *testing.T) {
	l := testRedis(t)
	rule := Rule{Name: "burst", Limit: 5, Window: time.Minute}
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d, err := l.Allow(context.Background(), rule, "same-ip"); err == nil && d.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := allowed.Load(); n != 5 {
		t.Errorf("%d of 100 concurrent requests allowed, want exactly 5", n)
	}
}

func TestRedisRepairsKeyWithoutExpiry(t *testing.T) {
	l := testRedis(t)
	ctx := context.Background()
	key := l.Prefix + ":register:stuck"
	// Simulate a counter left without a TTL by an older, non-atomic client.
	if err := l.Client.Set(ctx, key, 3, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Allow(ctx, Rule{Name: "register", Limit: 5, Window: time.Hour}, "stuck"); err != nil {
		t.Fatal(err)
	}
	if ttl, _ := l.Client.PTTL(ctx, key).Result(); ttl <= 0 {
		t.Errorf("TTL = %s; a key without expiry would block the IP forever", ttl)
	}
}

func TestRedisUnavailableReturnsError(t *testing.T) {
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond, MaxRetries: -1})
	defer c.Close()
	if _, err := (&Redis{Client: c}).Allow(context.Background(), registerRule, "k"); err == nil {
		t.Error("expected an error when Redis is unreachable")
	}
}
