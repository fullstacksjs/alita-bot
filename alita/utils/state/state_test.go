package state_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/state"
)

type mockClock struct {
	mu  sync.Mutex
	now time.Time
}

func newMockClock(start time.Time) *mockClock {
	return &mockClock{now: start}
}

func (m *mockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *mockClock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}

func TestSetAndTypedRetrieval(t *testing.T) {
	ctx := context.Background()

	state.SimulateRestart()
	defer state.SimulateRestart()

	state.Set(ctx, "str-key", "hello state", time.Minute)
	val, ok := state.Get[string](ctx, "str-key")
	if !ok || val != "hello state" {
		t.Fatalf("Get[string]() = (%q, %v), want (%q, true)", val, ok, "hello state")
	}

	state.Set(ctx, "int-key", 42, time.Minute)
	intVal, ok := state.Get[int](ctx, "int-key")
	if !ok || intVal != 42 {
		t.Fatalf("Get[int]() = (%d, %v), want (42, true)", intVal, ok)
	}

	// Type mismatch should return zero value and false
	mismatchVal, ok := state.Get[string](ctx, "int-key")
	if ok || mismatchVal != "" {
		t.Fatalf("Get[string](int-key) = (%q, %v), want empty string and false", mismatchVal, ok)
	}
}

func TestAtomicConsumeOnce(t *testing.T) {
	ctx := context.Background()

	state.SimulateRestart()
	defer state.SimulateRestart()

	const key = "token-123"
	state.Set(ctx, key, "secret-value", time.Minute)

	// First consume-once call must succeed
	val, ok := state.GetAndDelete[string](ctx, key)
	if !ok || val != "secret-value" {
		t.Fatalf("1st GetAndDelete[string]() = (%q, %v), want (%q, true)", val, ok, "secret-value")
	}

	// Second consume-once call must fail
	val2, ok2 := state.GetAndDelete[string](ctx, key)
	if ok2 || val2 != "" {
		t.Fatalf("2nd GetAndDelete[string]() = (%q, %v), want empty string and false", val2, ok2)
	}

	// Subsequent Get must fail
	if _, ok := state.Get[string](ctx, key); ok {
		t.Fatalf("Get() after consume-once found key, want not found")
	}
}

func TestDeletion(t *testing.T) {
	ctx := context.Background()

	state.SimulateRestart()
	defer state.SimulateRestart()

	const key = "delete-me"
	state.Set(ctx, key, "value", time.Minute)

	if !state.Delete(ctx, key) {
		t.Fatalf("Delete(%q) returned false, want true", key)
	}

	if _, ok := state.Get[string](ctx, key); ok {
		t.Fatalf("Get(%q) returned true after deletion, want false", key)
	}

	if state.Delete(ctx, key) {
		t.Fatalf("Delete(%q) of nonexistent key returned true, want false", key)
	}
}

func TestExpirySemantics(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := newMockClock(startTime)

	store := state.NewMemoryStore(
		state.WithClock(clock),
		state.WithCleanupInterval(0),
	)
	defer func() { _ = store.Close() }()

	const key = "ephemeral-key"
	state.SetIn(ctx, store, key, "ephemeral-val", 5*time.Second)

	// Before expiry
	val, ok := state.GetFrom[string](ctx, store, key)
	if !ok || val != "ephemeral-val" {
		t.Fatalf("GetFrom() before expiry = (%q, %v), want (%q, true)", val, ok, "ephemeral-val")
	}

	// Advance clock past expiry
	clock.Advance(6 * time.Second)

	// Get after expiry must return false
	if _, ok := state.GetFrom[string](ctx, store, key); ok {
		t.Fatalf("GetFrom() after expiry returned true, want false")
	}

	// GetAndDelete after expiry must return false
	if _, ok := state.GetAndDeleteFrom[string](ctx, store, key); ok {
		t.Fatalf("GetAndDeleteFrom() after expiry returned true, want false")
	}

	// Set with negative TTL should expire immediately
	const negKey = "neg-ttl-key"
	state.SetIn(ctx, store, negKey, "val", -1*time.Second)
	if _, ok := state.GetFrom[string](ctx, store, negKey); ok {
		t.Fatalf("GetFrom() with negative TTL returned true, want false")
	}
}

func TestBoundedCleanupMechanism(t *testing.T) {
	ctx := context.Background()
	startTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := newMockClock(startTime)

	store := state.NewMemoryStore(
		state.WithClock(clock),
		state.WithCleanupBatch(2),
		state.WithCleanupInterval(0),
	)
	defer func() { _ = store.Close() }()

	// Insert 5 entries with 1 second TTL
	for i := 0; i < 5; i++ {
		state.SetIn(ctx, store, fmt.Sprintf("key-%d", i), i, time.Second)
	}

	// Advance time to expire all keys
	clock.Advance(2 * time.Second)

	// Sweep should delete at most cleanupBatch (2) entries
	deletedCount := store.SweepExpired()
	if deletedCount != 2 {
		t.Fatalf("SweepExpired() deleted %d entries, want 2 (bounded batch)", deletedCount)
	}

	// Second sweep deletes another 2 entries
	deletedCount2 := store.SweepExpired()
	if deletedCount2 != 2 {
		t.Fatalf("2nd SweepExpired() deleted %d entries, want 2", deletedCount2)
	}

	// Final sweep deletes remaining 1 entry
	deletedCount3 := store.SweepExpired()
	if deletedCount3 != 1 {
		t.Fatalf("3rd SweepExpired() deleted %d entries, want 1", deletedCount3)
	}
}

func TestSimulateRestart(t *testing.T) {
	ctx := context.Background()

	state.SimulateRestart()
	defer state.SimulateRestart()

	state.Set(ctx, "session-1", "active", 10*time.Minute)
	state.Set(ctx, "counter-1", 100, 10*time.Minute)

	if _, ok := state.Get[string](ctx, "session-1"); !ok {
		t.Fatal("session-1 not found before restart")
	}

	// Simulate restart
	state.SimulateRestart()

	if _, ok := state.Get[string](ctx, "session-1"); ok {
		t.Fatal("session-1 persisted across SimulateRestart, want cleared")
	}
	if _, ok := state.Get[int](ctx, "counter-1"); ok {
		t.Fatal("counter-1 persisted across SimulateRestart, want cleared")
	}
}

func TestSetStoreAndRestore(t *testing.T) {
	ctx := context.Background()

	state.SimulateRestart()
	defer state.SimulateRestart()

	state.Set(ctx, "k1", "v1", time.Minute)

	customStore := state.NewMemoryStore()
	restore := state.SetStore(customStore)

	// Global API now operates on customStore
	if _, ok := state.Get[string](ctx, "k1"); ok {
		t.Fatal("k1 found in custom store before set, want missing")
	}

	state.Set(ctx, "k2", "v2", time.Minute)
	if val, ok := state.Get[string](ctx, "k2"); !ok || val != "v2" {
		t.Fatalf("k2 in custom store = (%q, %v), want (v2, true)", val, ok)
	}

	// Revert to original store
	restore()

	if val, ok := state.Get[string](ctx, "k1"); !ok || val != "v1" {
		t.Fatalf("k1 after restore = (%q, %v), want (v1, true)", val, ok)
	}
	if _, ok := state.Get[string](ctx, "k2"); ok {
		t.Fatal("k2 found after restoring original store, want missing")
	}
}

func TestConcurrentAccessAndExpirySafety(t *testing.T) {
	ctx := context.Background()

	store := state.NewMemoryStore(
		state.WithCleanupInterval(10 * time.Millisecond),
		state.WithCleanupBatch(100),
	)
	defer func() { _ = store.Close() }()

	const numGoroutines = 20
	const numOps = 200

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(g int) {
			defer wg.Done()

			for i := 0; i < numOps; i++ {
				key := fmt.Sprintf("key-%d", i%20)

				switch i % 4 {
				case 0:
					state.SetIn(ctx, store, key, fmt.Sprintf("val-%d-%d", g, i), 15*time.Millisecond)
				case 1:
					_, _ = state.GetFrom[string](ctx, store, key)
				case 2:
					_, _ = state.GetAndDeleteFrom[string](ctx, store, key)
				case 3:
					_ = state.DeleteFrom(ctx, store, key)
				}
			}
		}(g)
	}

	wg.Wait()
}
