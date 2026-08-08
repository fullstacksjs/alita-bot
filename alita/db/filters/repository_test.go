package filters

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

func skipIfNoDb(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
}

func newFilterTestChat(t *testing.T) int64 {
	t.Helper()
	chatID := -time.Now().UnixNano()
	if err := chats.EnsureChatInDb(chatID, "test-filters"); err != nil {
		t.Fatalf("EnsureChatInDb() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
			t.Errorf("cleanup Chat failed: %v", err)
		}
	})
	return chatID
}

func TestAddAndGetFiltersList(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)

	t.Cleanup(func() {
		if err := RemoveAllFilters(chatID); err != nil {
			t.Errorf("RemoveAllFilters failed: %v", err)
		}
	})

	// Initially empty
	list := GetFiltersList(chatID)
	if len(list) != 0 {
		t.Fatalf("expected empty filter list for new chat, got %d items", len(list))
	}

	// Add two filters
	if err := AddFilter(chatID, "spam", "spam reply", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}
	if err := AddFilter(chatID, "flood", "flood reply", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}

	list = GetFiltersList(chatID)
	if len(list) != 2 {
		t.Fatalf("expected 2 filters after adding, got %d", len(list))
	}

	// Adding the same keyword again must not bypass overwrite confirmation.
	if err := AddFilter(chatID, "spam", "different reply", "", nil, 2); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}
	list = GetFiltersList(chatID)
	if len(list) != 2 {
		t.Fatalf("expected 2 filters (no duplicate), got %d", len(list))
	}
	var updated models.ChatFilters
	if err := db.DB.Where("chat_id = ? AND keyword = ?", chatID, "spam").Take(&updated).Error; err != nil {
		t.Fatalf("read replaced filter failed: %v", err)
	}
	if updated.FilterReply != "spam reply" || updated.MsgType != 1 {
		t.Fatalf("filter = %+v, want original reply and type", updated)
	}
}

func TestDoesFilterExists(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)

	t.Cleanup(func() {
		if err := RemoveAllFilters(chatID); err != nil {
			t.Errorf("RemoveAllFilters failed: %v", err)
		}
	})

	if DoesFilterExists(chatID, "nonexistent") {
		t.Fatal("expected DoesFilterExists=false for non-existent filter")
	}

	if err := AddFilter(chatID, "hello", "hello reply", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}

	if !DoesFilterExists(chatID, "hello") {
		t.Fatal("expected DoesFilterExists=true after adding filter")
	}

	// Case-insensitive check
	if !DoesFilterExists(chatID, "HELLO") {
		t.Fatal("expected DoesFilterExists=true for uppercase variant (case-insensitive)")
	}
}

func TestRemoveFilter(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)

	t.Cleanup(func() {
		if err := RemoveAllFilters(chatID); err != nil {
			t.Errorf("RemoveAllFilters failed: %v", err)
		}
	})

	if err := AddFilter(chatID, "remove_me", "reply", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}
	if err := AddFilter(chatID, "keep_me", "reply", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}

	if err := RemoveFilter(chatID, "remove_me"); err != nil {
		t.Fatalf("RemoveFilter failed: %v", err)
	}

	if DoesFilterExists(chatID, "remove_me") {
		t.Fatal("expected filter to be removed")
	}
	if !DoesFilterExists(chatID, "keep_me") {
		t.Fatal("expected keep_me filter to still exist")
	}

	// Removing non-existent filter should not error
	if err := RemoveFilter(chatID, "does_not_exist"); err != nil {
		t.Fatalf("RemoveFilter(nonexistent) failed: %v", err)
	}
}

func TestRemoveAllFilters(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)

	if err := AddFilter(chatID, "a", "a", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}
	if err := AddFilter(chatID, "b", "b", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}
	if err := AddFilter(chatID, "c", "c", "", nil, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}

	if err := RemoveAllFilters(chatID); err != nil {
		t.Fatalf("RemoveAllFilters failed: %v", err)
	}

	list := GetFiltersList(chatID)
	if len(list) != 0 {
		t.Fatalf("expected empty list after RemoveAllFilters, got %d", len(list))
	}
}

func TestCountFilters(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)

	t.Cleanup(func() {
		if err := RemoveAllFilters(chatID); err != nil {
			t.Errorf("RemoveAllFilters failed: %v", err)
		}
	})

	if CountFilters(chatID) != 0 {
		t.Fatal("expected count=0 for new chat")
	}

	for i := 0; i < 3; i++ {
		if err := AddFilter(chatID, fmt.Sprintf("word%d", i), "reply", "", nil, 1); err != nil {
			t.Fatalf("AddFilter failed: %v", err)
		}
	}

	if CountFilters(chatID) != 3 {
		t.Fatalf("expected count=3, got %d", CountFilters(chatID))
	}
}

func TestLoadFilterStats(t *testing.T) {
	skipIfNoDb(t)

	// Just verify it returns non-negative values without panicking
	total, chats := LoadFilterStats()
	if total < 0 {
		t.Errorf("LoadFilterStats total = %d, want >= 0", total)
	}
	if chats < 0 {
		t.Errorf("LoadFilterStats chats = %d, want >= 0", chats)
	}
}

func TestLoadFilterStatsErrorBranch(t *testing.T) {
	skipIfNoDb(t)

	if err := db.DB.Migrator().DropTable(&models.ChatFilters{}); err != nil {
		t.Fatalf("DropTable failed: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(`CREATE TABLE IF NOT EXISTS filters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id BIGINT NOT NULL,
			keyword TEXT NOT NULL,
			filter_reply TEXT,
			msgtype INTEGER,
			fileid TEXT,
			nonotif BOOLEAN DEFAULT 0,
			filter_buttons TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
		);`)
		db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uk_filters_chat_keyword ON filters (chat_id, keyword);`)
	})

	if err := AddFilter(1, "missing-table", "reply", "", nil, db.TEXT); err == nil {
		t.Fatal("AddFilter() error = nil after filters table was dropped")
	}
	total, chats := LoadFilterStats()
	if total != 0 || chats != 0 {
		t.Fatalf("LoadFilterStats() = (%d, %d), want (0, 0) on error", total, chats)
	}
}

func TestAddFilterWithButtons(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)

	t.Cleanup(func() {
		if err := RemoveAllFilters(chatID); err != nil {
			t.Errorf("RemoveAllFilters failed: %v", err)
		}
	})

	buttons := []models.Button{
		{Name: "Click me", Url: "https://example.com", SameLine: false},
	}

	if err := AddFilter(chatID, "btn_filter", "Filter with button", "", buttons, 1); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}

	if !DoesFilterExists(chatID, "btn_filter") {
		t.Fatal("expected filter with buttons to exist")
	}
}

func TestAddFilterConcurrentInsert(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)
	t.Cleanup(func() {
		_ = RemoveAllFilters(chatID)
	})

	const writers = 16
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- AddFilter(chatID, "shared", "concurrent", "", nil, db.TEXT)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent AddFilter() error = %v", err)
		}
	}

	if err := AddFilter(chatID, "shared", "final", "", nil, db.TEXT); err != nil {
		t.Fatalf("final AddFilter() error = %v", err)
	}
	var rows []models.ChatFilters
	if err := db.DB.Where("chat_id = ? AND keyword = ?", chatID, "shared").Find(&rows).Error; err != nil {
		t.Fatalf("read filters error = %v", err)
	}
	if len(rows) != 1 || rows[0].FilterReply != "concurrent" {
		t.Fatalf("concurrent insert left filters=%+v", rows)
	}
}

func TestUpdateFilterAndConcurrentOps(t *testing.T) {
	skipIfNoDb(t)

	chatID := newFilterTestChat(t)
	t.Cleanup(func() {
		_ = RemoveAllFilters(chatID)
	})

	// Updating non-existent filter returns false
	updated, err := UpdateFilter(chatID, "ghost", "new text", "", nil, db.TEXT)
	if err != nil {
		t.Fatalf("UpdateFilter(ghost) unexpected error = %v", err)
	}
	if updated {
		t.Fatal("UpdateFilter(ghost) returned true, want false")
	}

	// Add initial filter
	if err := AddFilter(chatID, "test_item", "initial reply", "", nil, db.TEXT); err != nil {
		t.Fatalf("AddFilter failed: %v", err)
	}

	// Overwrite via UpdateFilter
	updated, err = UpdateFilter(chatID, "test_item", "updated reply", "file1", nil, db.PHOTO)
	if err != nil || !updated {
		t.Fatalf("UpdateFilter(test_item) updated=%v, err=%v", updated, err)
	}

	// Verify persistence
	var filter models.ChatFilters
	if err := db.DB.Where("chat_id = ? AND keyword = ?", chatID, "test_item").Take(&filter).Error; err != nil {
		t.Fatalf("read updated filter error = %v", err)
	}
	if filter.FilterReply != "updated reply" || filter.FileID != "file1" || filter.MsgType != db.PHOTO {
		t.Fatalf("updated filter state = %+v", filter)
	}

	// Concurrent operations: Add, Update, Remove, GetFiltersList
	const workers = 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*4)

	for i := range workers {
		wg.Add(4)
		word := fmt.Sprintf("conc_%d", i)
		go func(w string) {
			defer wg.Done()
			if err := AddFilter(chatID, w, "reply", "", nil, db.TEXT); err != nil {
				errs <- fmt.Errorf("AddFilter(%s): %w", w, err)
			}
		}(word)
		go func(w string) {
			defer wg.Done()
			_, _ = UpdateFilter(chatID, w, "new_reply", "", nil, db.TEXT)
		}(word)
		go func(w string) {
			defer wg.Done()
			_ = GetFiltersList(chatID)
		}(word)
		go func(w string) {
			defer wg.Done()
			_ = RemoveFilter(chatID, w)
		}(word)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
