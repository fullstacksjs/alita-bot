package reactions

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

func skipIfNoDb(t *testing.T) {
	t.Helper()
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
}

func TestReactionsRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	_ = chats.EnsureChatInDb(chatID, "test-reactions")
	t.Cleanup(func() {
		_ = ResetReactions(chatID)
		_ = db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error
		cache.DeleteCache(cache.CacheKey("reactions", chatID))
	})

	if got := GetReactions(chatID); len(got) != 0 {
		t.Fatalf("GetReactions on fresh chat = %v, want empty", got)
	}

	if err := AddReaction(chatID, "Hello", "👋"); err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
	if err := AddReaction(chatID, "bye ", "🚀"); err != nil {
		t.Fatalf("AddReaction(second) error = %v", err)
	}
	if got := GetReactions(chatID); got["hello"] != "👋" || got["bye"] != "🚀" {
		t.Fatalf("GetReactions = %v, want hello=👋 bye=🚀 (normalized keywords)", got)
	}

	// Upsert: re-adding the same keyword updates the emoji, not a duplicate.
	if err := AddReaction(chatID, "HELLO", "😀"); err != nil {
		t.Fatalf("AddReaction(upsert) error = %v", err)
	}
	if got := GetReactions(chatID); got["hello"] != "😀" || len(got) != 2 {
		t.Fatalf("GetReactions after upsert = %v, want hello=😀 and 2 entries", got)
	}

	if err := RemoveReaction(chatID, "BYE"); err != nil {
		t.Fatalf("RemoveReaction() error = %v", err)
	}
	got := GetReactions(chatID)
	if _, ok := got["bye"]; ok || len(got) != 1 {
		t.Fatalf("GetReactions after remove = %v, want only hello", got)
	}

	if err := ResetReactions(chatID); err != nil {
		t.Fatalf("ResetReactions() error = %v", err)
	}
	if got := GetReactions(chatID); len(got) != 0 {
		t.Fatalf("GetReactions after reset = %v, want empty", got)
	}
}

func TestReactionsConcurrentWrites(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	_ = chats.EnsureChatInDb(chatID, "test-reactions-concurrent")
	t.Cleanup(func() {
		_ = ResetReactions(chatID)
		_ = db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error
		cache.DeleteCache(cache.CacheKey("reactions", chatID))
	})

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers*3)

	for i := range workers {
		wg.Add(3)
		kw := fmt.Sprintf("kw_%d", i)
		go func(k string) {
			defer wg.Done()
			if err := AddReaction(chatID, k, "👍"); err != nil {
				errs <- fmt.Errorf("AddReaction(%s): %w", k, err)
			}
		}(kw)
		go func(k string) {
			defer wg.Done()
			_ = GetReactions(chatID)
		}(kw)
		go func(k string) {
			defer wg.Done()
			_ = RemoveReaction(chatID, k)
		}(kw)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

