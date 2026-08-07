package state

import (
	"context"
	"sync"
	"time"
)

// Store defines the interface for an in-process state store with TTL capabilities.
type Store interface {
	Get(ctx context.Context, key string) (any, bool)
	Set(ctx context.Context, key string, value any, ttl time.Duration)
	GetAndDelete(ctx context.Context, key string) (any, bool)
	Delete(ctx context.Context, key string) bool
	Clear(ctx context.Context)
	Close() error
}

var (
	defaultStore Store
	storeMu      sync.RWMutex
)

func init() {
	defaultStore = NewMemoryStore()
}

// GetStore returns the active global Store instance.
func GetStore() Store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return defaultStore
}

// SetStore replaces the global Store instance and returns a restore function.
// The caller is responsible for managing the lifetime of replaced stores.
func SetStore(s Store) func() {
	storeMu.Lock()
	prev := defaultStore
	defaultStore = s
	storeMu.Unlock()

	return func() {
		storeMu.Lock()
		if defaultStore != nil && defaultStore != prev {
			_ = defaultStore.Close()
		}
		defaultStore = prev
		storeMu.Unlock()
	}
}

// SimulateRestart replaces the active Store with a fresh MemoryStore instance,
// closing the previous store to clear all ephemeral state.
func SimulateRestart() {
	newStore := NewMemoryStore()
	storeMu.Lock()
	prev := defaultStore
	defaultStore = newStore
	storeMu.Unlock()

	if prev != nil {
		_ = prev.Close()
	}
}

// Get retrieves a typed value for key from the active default store.
// Returns zero value and false if key is missing, expired, or of a different type.
func Get[T any](ctx context.Context, key string) (T, bool) {
	return GetFrom[T](ctx, GetStore(), key)
}

// GetFrom retrieves a typed value for key from the provided store.
func GetFrom[T any](ctx context.Context, s Store, key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}

	val, ok := s.Get(ctx, key)
	if !ok {
		return zero, false
	}

	typed, ok := val.(T)
	if !ok {
		return zero, false
	}

	return typed, true
}

// Set stores a value with a TTL in the active default store.
func Set[T any](ctx context.Context, key string, val T, ttl time.Duration) {
	SetIn[T](ctx, GetStore(), key, val, ttl)
}

// SetIn stores a value with a TTL in the provided store.
func SetIn[T any](ctx context.Context, s Store, key string, val T, ttl time.Duration) {
	if s == nil {
		return
	}
	s.Set(ctx, key, val, ttl)
}

// GetAndDelete atomically retrieves and removes a typed value for key from the active default store.
// Returns zero value and false if key is missing, expired, or of a different type.
func GetAndDelete[T any](ctx context.Context, key string) (T, bool) {
	return GetAndDeleteFrom[T](ctx, GetStore(), key)
}

// GetAndDeleteFrom atomically retrieves and removes a typed value for key from the provided store.
func GetAndDeleteFrom[T any](ctx context.Context, s Store, key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}

	val, ok := s.GetAndDelete(ctx, key)
	if !ok {
		return zero, false
	}

	typed, ok := val.(T)
	if !ok {
		return zero, false
	}

	return typed, true
}

// Delete removes a key from the active default store.
func Delete(ctx context.Context, key string) bool {
	return DeleteFrom(ctx, GetStore(), key)
}

// DeleteFrom removes a key from the provided store.
func DeleteFrom(ctx context.Context, s Store, key string) bool {
	if s == nil {
		return false
	}
	return s.Delete(ctx, key)
}

// Clear clears all entries from the active default store.
func Clear(ctx context.Context) {
	s := GetStore()
	if s != nil {
		s.Clear(ctx)
	}
}
