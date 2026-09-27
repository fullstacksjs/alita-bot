package ratelimit

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/state"
)

type tokenBucketState struct {
	Tokens float64
	Last   time.Time
}

// TokenBucketLimiter limits a named command or operation with a fixed capacity
// and a continuous refill over the given period. Use a distinct namespace for
// each policy; keys identify users, chats, or any other subject within it.
type TokenBucketLimiter struct {
	namespace    string
	capacity     int
	refillPeriod time.Duration
}

// Shared locking keeps reservations atomic even if callers create two limiters
// for the same namespace. The state store expires idle buckets after one refill period.
var tokenBucketMu sync.Mutex

// NewTokenBucketLimiter creates a limiter. Invalid fixed policy values panic.
func NewTokenBucketLimiter(namespace string, capacity int, refillPeriod time.Duration) *TokenBucketLimiter {
	if namespace == "" || capacity <= 0 || refillPeriod <= 0 {
		panic("ratelimit: token bucket requires a namespace, positive capacity, and positive refill period")
	}
	return &TokenBucketLimiter{namespace: namespace, capacity: capacity, refillPeriod: refillPeriod}
}

// Acquire consumes one token for key and returns the wait until the next token
// when the bucket is empty. An empty key shares one bucket across all callers.
func (r *TokenBucketLimiter) Acquire(key string) (bool, time.Duration) {
	return r.acquireAt(key, time.Now())
}

func (r *TokenBucketLimiter) acquireAt(key string, now time.Time) (bool, time.Duration) {
	stateKey := fmt.Sprintf("tokenbucket:%d:%s:%s", len(r.namespace), r.namespace, key)
	tokenBucketMu.Lock()
	defer tokenBucketMu.Unlock()

	ctx := context.Background()
	store := state.GetStore()
	bucket, found := state.GetFrom[tokenBucketState](ctx, store, stateKey)
	if !found {
		bucket = tokenBucketState{Tokens: float64(r.capacity), Last: now}
	} else if elapsed := now.Sub(bucket.Last); elapsed > 0 {
		bucket.Tokens = math.Min(float64(r.capacity), bucket.Tokens+float64(r.capacity)*float64(elapsed)/float64(r.refillPeriod))
		bucket.Last = now
	}
	if bucket.Tokens < 1 {
		wait := time.Duration(math.Ceil((1 - bucket.Tokens) * float64(r.refillPeriod) / float64(r.capacity)))
		state.SetIn(ctx, store, stateKey, bucket, r.refillPeriod)
		return false, wait
	}
	bucket.Tokens--
	state.SetIn(ctx, store, stateKey, bucket, r.refillPeriod)
	return true, 0
}
