package cache

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync/atomic"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/error_handling"
	"github.com/divkix/Alita_Robot/alita/utils/state"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

// generationShards bounds invalidation bookkeeping to a fixed amount of memory
// while keeping unrelated writes from suppressing each other's cache fills.
const generationShards = 256

var (
	cacheGroup     singleflight.Group
	generations    [generationShards]atomic.Uint64
	cachingEnabled atomic.Bool
)

// cacheCtx is the background context used for in-process state access.
// The state store never blocks, so no request-scoped context is required.
var cacheCtx = context.Background()

func init() {
	cachingEnabled.Store(true)
}

// generationFor returns the invalidation counter guarding a cache key.
func generationFor(key string) *atomic.Uint64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &generations[h.Sum32()%generationShards]
}

// Enabled reports whether repository read-through caching is active.
func Enabled() bool {
	return cachingEnabled.Load()
}

// SetEnabled toggles repository read-through caching and returns a function
// that restores the previous setting. Disabling makes every read fall through
// to the loader, which is what tests that assert on database state want.
func SetEnabled(enabled bool) (restore func()) {
	previous := cachingEnabled.Swap(enabled)
	return func() {
		cachingEnabled.Store(previous)
	}
}

// loadResult carries the invalidation generation a shared load observed, so a
// caller can tell whether that load predates a write it must already see.
type loadResult struct {
	val any
	gen uint64
}

// GetFromCacheOrLoad is a generic helper to get from the in-process cache or
// load from the database with stampede protection.
//
// Cached values are shared between callers instead of being deserialized per
// read, so callers must treat the returned value as read-only.
func GetFromCacheOrLoad[T any](key string, ttl time.Duration, loader func() (T, error)) (T, error) {
	var result T

	if !Enabled() || state.GetStore() == nil {
		return loader()
	}

	if cached, ok := state.Get[T](cacheCtx, key); ok {
		return cached, nil
	}

	generation := generationFor(key)
	callerGen := generation.Load()

	resCh := make(chan struct {
		val T
		err error
	}, 1)

	go func() {
		defer error_handling.RecoverFromPanic("cache", "GetFromCacheOrLoad")

		v, err, shared := cacheGroup.Do(key, func() (interface{}, error) {
			loadGen := generation.Load()
			val, err := loader()
			if err != nil {
				return nil, err
			}

			if loadGen == generation.Load() {
				state.Set(cacheCtx, key, val, ttl)
				if loadGen != generation.Load() {
					// An invalidation raced with Set after the first check. Delete
					// the value so an old database snapshot cannot survive it.
					state.Delete(cacheCtx, key)
				}
			}
			return loadResult{val: val, gen: loadGen}, nil
		})

		if shared {
			log.Debugf("[Cache] Shared cache load for key: %s", key)
		}

		if err != nil {
			resCh <- struct {
				val T
				err error
			}{result, err}
			return
		}

		res, ok := v.(loadResult)
		if !ok {
			resCh <- struct {
				val T
				err error
			}{result, fmt.Errorf("cache load for key %s returned %T, want %T", key, v, res)}
			return
		}

		if res.gen < callerGen {
			// The shared load started before an invalidation this caller must
			// already observe (for example its own write), so its snapshot is
			// not safe to reuse. Load once more, uncached.
			val, loadErr := loader()
			resCh <- struct {
				val T
				err error
			}{val, loadErr}
			return
		}

		typed, ok := res.val.(T)
		if !ok {
			resCh <- struct {
				val T
				err error
			}{result, fmt.Errorf("cache load for key %s returned %T, want %T", key, res.val, result)}
			return
		}

		resCh <- struct {
			val T
			err error
		}{typed, nil}
	}()

	select {
	case res := <-resCh:
		return res.val, res.err
	case <-time.After(30 * time.Second):
		cacheGroup.Forget(key)
		log.Errorf("[Cache] Timeout loading key %s after 30s", key)
		return result, fmt.Errorf("cache load timeout for key %s", key)
	}
}

// DeleteCache is a helper to delete a value from cache.
func DeleteCache(key string) {
	// Increment before deleting so an already-running loader cannot repopulate
	// the key with a database snapshot read before the write committed.
	generationFor(key).Add(1)

	state.Delete(cacheCtx, key)
}
