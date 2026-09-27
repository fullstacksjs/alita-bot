package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/state"
)

func TestTokenBucketLimiter(t *testing.T) {
	state.SimulateRestart()
	t.Cleanup(state.SimulateRestart)
	now := time.Unix(1_700_000_000, 0)
	member := NewTokenBucketLimiter("test:member", 10, 24*time.Hour)
	admin := NewTokenBucketLimiter("test:admin", 100, 24*time.Hour)

	for i := 0; i < 10; i++ {
		if allowed, _ := member.acquireAt("42", now); !allowed {
			t.Fatalf("member request %d was denied", i+1)
		}
	}
	if allowed, wait := member.acquireAt("42", now); allowed || wait != 144*time.Minute {
		t.Fatalf("exhausted member bucket = (%v, %v), want (false, 144m)", allowed, wait)
	}
	if allowed, _ := member.acquireAt("42", now.Add(144*time.Minute)); !allowed {
		t.Fatal("one member token should refill after 144 minutes")
	}
	if allowed, _ := member.acquireAt("another-user", now); !allowed {
		t.Fatal("different keys should have independent buckets")
	}

	for i := 0; i < 100; i++ {
		if allowed, _ := admin.acquireAt("42", now); !allowed {
			t.Fatalf("admin request %d was denied", i+1)
		}
	}
	if allowed, wait := admin.acquireAt("42", now); allowed || wait != 14*time.Minute+24*time.Second {
		t.Fatalf("exhausted admin bucket = (%v, %v), want (false, 14m24s)", allowed, wait)
	}
}

func TestTokenBucketLimiterCustomRefill(t *testing.T) {
	state.SimulateRestart()
	t.Cleanup(state.SimulateRestart)
	limiter := NewTokenBucketLimiter("test:hourly", 2, time.Hour)
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 2; i++ {
		if allowed, _ := limiter.acquireAt("user", now); !allowed {
			t.Fatalf("request %d was denied", i+1)
		}
	}
	if allowed, wait := limiter.acquireAt("user", now); allowed || wait != 30*time.Minute {
		t.Fatalf("exhausted hourly bucket = (%v, %v), want (false, 30m)", allowed, wait)
	}
	if allowed, _ := limiter.acquireAt("user", now.Add(30*time.Minute)); !allowed {
		t.Fatal("one token should refill after 30 minutes")
	}
}

func TestTokenBucketLimiterConcurrentInstances(t *testing.T) {
	state.SimulateRestart()
	t.Cleanup(state.SimulateRestart)
	first := NewTokenBucketLimiter("test:shared", 10, 24*time.Hour)
	second := NewTokenBucketLimiter("test:shared", 10, 24*time.Hour)
	now := time.Unix(1_700_000_000, 0)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowedCount := 0
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			limiter := first
			if index%2 == 0 {
				limiter = second
			}
			allowed, _ := limiter.acquireAt("44", now)
			if allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if allowedCount != 10 {
		t.Fatalf("concurrent allowed requests = %d, want 10", allowedCount)
	}
}
