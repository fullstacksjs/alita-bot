//go:build testtools

package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/state"
)

func TestDeleteCachePreventsInFlightLoaderFromRepopulatingStaleValue(t *testing.T) {
	state.SimulateRestart()

	const key = "alita:test:loader-race"
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan string, 1)

	go func() {
		got, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
			close(started)
			<-release
			return "stale", nil
		})
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- got
	}()

	<-started
	DeleteCache(key)
	close(release)

	if got := <-result; got != "stale" {
		t.Fatalf("GetFromCacheOrLoad() = %q, want caller snapshot stale", got)
	}

	if cached, ok := state.Get[string](context.Background(), key); ok {
		t.Fatalf("cache contains %q after invalidation raced with load", cached)
	}
}

func TestGetFromCacheOrLoadServesInProcessHitsAndCollapsesConcurrentLoads(t *testing.T) {
	state.SimulateRestart()

	const key = "alita:test:loader-inprocess"
	var loads int64
	var mu sync.Mutex
	release := make(chan struct{})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
				mu.Lock()
				loads++
				mu.Unlock()
				<-release
				return "value", nil
			})
			if err != nil || got != "value" {
				t.Errorf("GetFromCacheOrLoad() = (%q, %v), want (value, nil)", got, err)
			}
		}()
	}

	// Give the goroutines time to join the same singleflight load.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	gotLoads := loads
	mu.Unlock()
	if gotLoads != 1 {
		t.Fatalf("loader calls = %d, want 1 collapsed load", gotLoads)
	}

	// A subsequent read is served from the in-process store without loading.
	got, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
		t.Error("loader called on cache hit")
		return "", nil
	})
	if err != nil || got != "value" {
		t.Fatalf("GetFromCacheOrLoad() cached = (%q, %v), want (value, nil)", got, err)
	}
}

// A caller that invalidates the key and then reads must never be handed the
// snapshot of a shared load that started before its own write.
func TestGetFromCacheOrLoadDoesNotServeSharedLoadFromBeforeCallersWrite(t *testing.T) {
	state.SimulateRestart()

	const key = "alita:test:loader-read-your-write"
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
			close(started)
			<-release
			return "before-write", nil
		})
	}()

	<-started

	// Simulate a write that commits and invalidates while the load above is in
	// flight, then read from the same goroutine that performed the write.
	DeleteCache(key)

	result := make(chan string, 1)
	go func() {
		got, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
			return "after-write", nil
		})
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- got
	}()

	// Give the second caller time to join the in-flight singleflight load.
	time.Sleep(50 * time.Millisecond)
	close(release)

	if got := <-result; got != "after-write" {
		t.Fatalf("GetFromCacheOrLoad() after write = %q, want after-write", got)
	}
	<-done
}

func TestGetFromCacheOrLoadFallsBackWhenCachingDisabled(t *testing.T) {
	state.SimulateRestart()
	restore := SetEnabled(false)
	defer restore()

	const key = "alita:test:loader-disabled"
	loads := 0
	for range 3 {
		got, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
			loads++
			return "fresh", nil
		})
		if err != nil || got != "fresh" {
			t.Fatalf("GetFromCacheOrLoad() = (%q, %v), want (fresh, nil)", got, err)
		}
	}

	if loads != 3 {
		t.Fatalf("loader calls = %d, want 3 database fallbacks", loads)
	}
	if _, ok := state.Get[string](context.Background(), key); ok {
		t.Fatal("disabled caching stored a value in the state store")
	}
}

func TestGetFromCacheOrLoadClearsOnSimulatedRestart(t *testing.T) {
	state.SimulateRestart()

	const key = "alita:test:loader-restart"
	if _, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
		return "before", nil
	}); err != nil {
		t.Fatalf("GetFromCacheOrLoad() error = %v", err)
	}

	state.SimulateRestart()

	got, err := GetFromCacheOrLoad(key, time.Minute, func() (string, error) {
		return "after", nil
	})
	if err != nil || got != "after" {
		t.Fatalf("GetFromCacheOrLoad() after restart = (%q, %v), want (after, nil)", got, err)
	}
}
