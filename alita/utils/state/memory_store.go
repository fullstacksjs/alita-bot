package state

import (
	"context"
	"sync"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/error_handling"
)

// Clock abstracts time fetching for deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

type entry struct {
	value     any
	expiresAt time.Time
}

// MemoryStore is a concurrency-safe in-process state store supporting TTL expiration.
type MemoryStore struct {
	mu              sync.RWMutex
	items           map[string]entry
	cleanupInterval time.Duration
	cleanupBatch    int
	clock           Clock
	stopCh          chan struct{}
	doneCh          chan struct{}
	closeOnce       sync.Once
}

// Option configures a MemoryStore.
type Option func(*MemoryStore)

// WithCleanupInterval sets the period for the background cleanup loop.
// Set to 0 to disable automatic background cleanup.
func WithCleanupInterval(d time.Duration) Option {
	return func(s *MemoryStore) {
		s.cleanupInterval = d
	}
}

// WithCleanupBatch sets the maximum number of expired entries deleted per sweep.
func WithCleanupBatch(batch int) Option {
	return func(s *MemoryStore) {
		s.cleanupBatch = batch
	}
}

// WithClock sets a custom Clock for time calculations.
func WithClock(clock Clock) Option {
	return func(s *MemoryStore) {
		s.clock = clock
	}
}

// NewMemoryStore constructs a new MemoryStore.
func NewMemoryStore(opts ...Option) *MemoryStore {
	s := &MemoryStore{
		items:           make(map[string]entry),
		cleanupInterval: 1 * time.Minute,
		cleanupBatch:    1000,
		clock:           realClock{},
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.cleanupInterval > 0 {
		go s.startCleanupLoop()
	} else {
		close(s.doneCh)
	}

	return s
}

func (s *MemoryStore) now() time.Time {
	if s.clock != nil {
		return s.clock.Now()
	}
	return time.Now()
}

func (s *MemoryStore) startCleanupLoop() {
	defer error_handling.RecoverFromPanic("startCleanupLoop", "state")
	defer close(s.doneCh)

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.SweepExpired()
		}
	}
}

// SweepExpired purges up to cleanupBatch expired entries from the store.
// Returns the number of entries removed.
func (s *MemoryStore) SweepExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	deleted := 0

	for k, item := range s.items {
		if !item.expiresAt.IsZero() && !now.Before(item.expiresAt) {
			delete(s.items, k)
			deleted++
			if s.cleanupBatch > 0 && deleted >= s.cleanupBatch {
				break
			}
		}
	}

	return deleted
}

// Get retrieves an entry by key if it exists and has not expired.
func (s *MemoryStore) Get(_ context.Context, key string) (any, bool) {
	s.mu.RLock()
	item, ok := s.items[key]
	if !ok {
		s.mu.RUnlock()
		return nil, false
	}

	now := s.now()
	if !item.expiresAt.IsZero() && !now.Before(item.expiresAt) {
		s.mu.RUnlock()
		s.mu.Lock()
		if item2, ok2 := s.items[key]; ok2 && !item2.expiresAt.IsZero() && !s.now().Before(item2.expiresAt) {
			delete(s.items, key)
		}
		s.mu.Unlock()
		return nil, false
	}

	s.mu.RUnlock()
	return item.value, true
}

// Set stores a key-value pair with a TTL duration.
// A ttl <= 0 indicates no expiration if ttl == 0, or already expired if ttl < 0.
func (s *MemoryStore) Set(_ context.Context, key string, value any, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = s.now().Add(ttl)
	} else if ttl < 0 {
		expiresAt = s.now().Add(-1 * time.Second)
	}

	s.items[key] = entry{
		value:     value,
		expiresAt: expiresAt,
	}
}

// SetNX stores a value with a TTL duration if the key does not exist or has expired.
// Returns true if the key was set, false otherwise.
func (s *MemoryStore) SetNX(_ context.Context, key string, value any, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	item, ok := s.items[key]
	if ok && (item.expiresAt.IsZero() || now.Before(item.expiresAt)) {
		return false
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	} else if ttl < 0 {
		expiresAt = now.Add(-1 * time.Second)
	}

	s.items[key] = entry{
		value:     value,
		expiresAt: expiresAt,
	}
	return true
}

// GetAndDelete atomically retrieves and removes an entry by key if it exists and has not expired.
func (s *MemoryStore) GetAndDelete(_ context.Context, key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[key]
	if !ok {
		return nil, false
	}

	delete(s.items, key)

	if !item.expiresAt.IsZero() && !s.now().Before(item.expiresAt) {
		return nil, false
	}

	return item.value, true
}

// Delete removes an entry by key. Returns true if the key existed.
func (s *MemoryStore) Delete(_ context.Context, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.items[key]
	delete(s.items, key)
	return ok
}

// Clear removes all entries from the store.
func (s *MemoryStore) Clear(_ context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = make(map[string]entry)
}

// Close stops the background cleanup loop and releases resources.
func (s *MemoryStore) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
	return nil
}
