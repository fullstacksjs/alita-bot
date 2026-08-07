package modules

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecentJoinProcessingNoCacheFallback(t *testing.T) {
	t.Cleanup(func() {
		clearRecentJoinProcessing(-100123, 456)
	})

	key := recentJoinProcessingKey(-100123, 456)
	if key != "alita:recentJoinProcessing:-100123:456" {
		t.Fatalf("recentJoinProcessingKey() = %q", key)
	}

	if !claimRecentJoinProcessing(-100123, 456) {
		t.Fatal("first claimRecentJoinProcessing() = false, want true")
	}
	if claimRecentJoinProcessing(-100123, 456) {
		t.Fatal("second claimRecentJoinProcessing() = true, want false duplicate")
	}

	clearRecentJoinProcessing(-100123, 456)
	if !claimRecentJoinProcessing(-100123, 456) {
		t.Fatal("claim after clear = false, want true")
	}
}

// TestClaimRecentJoinProcessingIsAtomic verifies that exactly one caller wins the
// join-dedup claim for a given (chat, user) pair — both in the sequential case
// and when N goroutines race concurrently.
func TestClaimRecentJoinProcessingIsAtomic(t *testing.T) {
	// Use unique IDs so this test doesn't collide with others.
	chatID := -time.Now().UnixNano() / 1e6 // negative to avoid valid Telegram chat IDs
	userID := time.Now().UnixNano()/1e6 + 1

	t.Cleanup(func() {
		clearRecentJoinProcessing(chatID, userID)
	})

	// Sequential: first call must win, second must lose.
	if !claimRecentJoinProcessing(chatID, userID) {
		t.Fatal("first claimRecentJoinProcessing() = false, want true")
	}
	if claimRecentJoinProcessing(chatID, userID) {
		t.Fatal("second claimRecentJoinProcessing() = true, want false (duplicate)")
	}

	// Reset for the concurrent sub-test.
	clearRecentJoinProcessing(chatID, userID)

	// Concurrent: N goroutines race; exactly one must win.
	const N = 20
	var wg sync.WaitGroup
	var wins atomic.Int64

	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			if claimRecentJoinProcessing(chatID, userID) {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("concurrent claimRecentJoinProcessing() winners = %d, want exactly 1", got)
	}
}
