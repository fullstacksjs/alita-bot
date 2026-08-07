package blacklists

import (
	"sync"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

func skipIfNoDb(t *testing.T) {
	t.Helper()
	if db.DB == nil {
		t.Skip("requires database connection")
	}
}

// blacklistsTableDDL mirrors the blacklists section of the SQLite baseline.
const blacklistsTableDDL = `
CREATE TABLE IF NOT EXISTS blacklists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id BIGINT NOT NULL,
    word TEXT NOT NULL,
    action TEXT DEFAULT 'warn' CHECK (action IN ('warn','mute','ban','kick','tban','tmute','delete','none')),
    reason TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id) ON DELETE CASCADE
)`

// restoreBlacklistsTable rebuilds the table with the constraints the baseline
// migration defines, which AutoMigrate cannot reproduce from the model alone.
func restoreBlacklistsTable(t *testing.T) {
	t.Helper()

	if err := db.DB.Exec(blacklistsTableDDL).Error; err != nil {
		t.Fatalf("restore blacklists table failed: %v", err)
	}
	if err := db.DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uk_blacklists_chat_word ON blacklists (chat_id, word)").Error; err != nil {
		t.Fatalf("restore blacklists index failed: %v", err)
	}
}

func TestAddBlacklistTrigger(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()

	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := AddBlacklist(chatID, "badword"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}

	settings := GetBlacklistSettings(chatID)
	if len(settings) != 1 {
		t.Fatalf("expected 1 blacklist entry, got %d", len(settings))
	}
	if settings[0].Word != "badword" {
		t.Fatalf("expected Word=%q, got %q", "badword", settings[0].Word)
	}
	if settings[0].Action != "warn" {
		t.Fatalf("expected default Action='warn', got %q", settings[0].Action)
	}
}

func TestRemoveBlacklistTrigger(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()

	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := AddBlacklist(chatID, "remove-me"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}
	if err := AddBlacklist(chatID, "keep-me"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}

	if err := RemoveBlacklist(chatID, "remove-me"); err != nil {
		t.Fatalf("RemoveBlacklist() error = %v", err)
	}

	settings := GetBlacklistSettings(chatID)
	for _, s := range settings {
		if s.Word == "remove-me" {
			t.Fatalf("expected 'remove-me' to be deleted from blacklist")
		}
	}

	found := false
	for _, s := range settings {
		if s.Word == "keep-me" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'keep-me' to still be in blacklist")
	}
}

func TestGetBlacklistSettings(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()

	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	// Empty chat should return empty slice, not nil
	settings := GetBlacklistSettings(chatID)
	if settings == nil {
		t.Fatalf("GetBlacklistSettings() returned nil, expected empty slice")
	}
	if len(settings) != 0 {
		t.Fatalf("expected 0 blacklist entries for new chat, got %d", len(settings))
	}
}

func TestSetBlacklistAction(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()

	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := AddBlacklist(chatID, "word1"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}
	if err := AddBlacklist(chatID, "word2"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}

	err := SetBlacklistAction(chatID, "ban")
	if err != nil {
		t.Fatalf("SetBlacklistAction() error = %v", err)
	}

	settings := GetBlacklistSettings(chatID)
	for _, s := range settings {
		if s.Action != "ban" {
			t.Fatalf("expected Action='ban' for all entries, got %q for word=%q", s.Action, s.Word)
		}
	}
}

func TestGetAllBlacklists(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()

	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	words := []string{"alpha", "beta", "gamma"}
	for _, w := range words {
		if err := AddBlacklist(chatID, w); err != nil {
			t.Fatalf("AddBlacklist() error = %v", err)
		}
	}

	settings := GetBlacklistSettings(chatID)
	if len(settings) < 3 {
		t.Fatalf("expected at least 3 blacklist entries, got %d", len(settings))
	}

	found := map[string]bool{}
	for _, s := range settings {
		found[s.Word] = true
	}
	for _, w := range words {
		if !found[w] {
			t.Fatalf("expected word %q in blacklist, not found", w)
		}
	}
}

func TestLoadBlacklistStats(t *testing.T) {
	skipIfNoDb(t)

	triggers, chats := LoadBlacklistsStats()
	if triggers < 0 {
		t.Errorf("LoadBlacklistsStats triggers = %d, want >= 0", triggers)
	}
	if chats < 0 {
		t.Errorf("LoadBlacklistsStats chats = %d, want >= 0", chats)
	}
}

func TestLoadBlacklistStatsErrorBranch(t *testing.T) {
	skipIfNoDb(t)

	_ = db.DB.Migrator().DropTable(&models.BlacklistSettings{})
	t.Cleanup(func() {
		// Restore the baseline DDL rather than AutoMigrate: the foreign key and
		// the (chat_id, word) unique index are not derivable from the model.
		restoreBlacklistsTable(t)
	})

	triggers, chats := LoadBlacklistsStats()
	if triggers != 0 || chats != 0 {
		t.Fatalf("LoadBlacklistsStats() = (%d, %d), want (0, 0) on error", triggers, chats)
	}
}

func TestBlacklistTriggerLowercased(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()

	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := AddBlacklist(chatID, "BadWord"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}

	settings := GetBlacklistSettings(chatID)
	if len(settings) != 1 {
		t.Fatalf("expected 1 blacklist entry, got %d", len(settings))
	}
	if settings[0].Word != "badword" {
		t.Fatalf("expected trigger to be lowercased to 'badword', got %q", settings[0].Word)
	}
}

// TestAddBlacklistIsUniquePerChatWord relies on the (chat_id, word) unique index
// to keep a re-added trigger idempotent instead of duplicating the row.
func TestAddBlacklistIsUniquePerChatWord(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()
	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	for range 3 {
		if err := AddBlacklist(chatID, "Repeated"); err != nil {
			t.Fatalf("AddBlacklist() error = %v", err)
		}
	}

	settings := GetBlacklistSettings(chatID)
	if len(settings) != 1 {
		t.Fatalf("expected 1 blacklist entry after repeated adds, got %d", len(settings))
	}

	// A raw duplicate insert must be rejected by the database itself.
	err := db.DB.Create(&models.BlacklistSettings{ChatId: chatID, Word: "repeated", Action: "warn"}).Error
	if err == nil {
		t.Fatal("duplicate insert = nil error, want unique constraint violation")
	}
}

// TestAddBlacklistInheritsSelectedAction keeps a chat's selected action applied
// to triggers added after the action was chosen.
func TestAddBlacklistInheritsSelectedAction(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()
	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := AddBlacklist(chatID, "first"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}
	if err := SetBlacklistAction(chatID, "ban"); err != nil {
		t.Fatalf("SetBlacklistAction() error = %v", err)
	}
	if err := AddBlacklist(chatID, "second"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}

	settings := GetBlacklistSettings(chatID)
	if len(settings) != 2 {
		t.Fatalf("expected 2 blacklist entries, got %d", len(settings))
	}
	for _, s := range settings {
		if s.Action != "ban" {
			t.Fatalf("Action = %q for word %q, want %q", s.Action, s.Word, "ban")
		}
	}
	if settings.Action() != "ban" {
		t.Fatalf("selected action = %q, want %q", settings.Action(), "ban")
	}
}

// TestBlacklistActionRejectsUnknownValue checks the SQLite CHECK constraint.
func TestBlacklistActionRejectsUnknownValue(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()
	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := AddBlacklist(chatID, "word"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}
	if err := SetBlacklistAction(chatID, "explode"); err == nil {
		t.Fatal("SetBlacklistAction(invalid) = nil, want constraint violation")
	}
}

// TestBlacklistEntriesRemovedWithChat exercises the ON DELETE CASCADE contract.
func TestBlacklistEntriesRemovedWithChat(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()
	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
		_ = db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error
	})

	if err := AddBlacklist(chatID, "doomed"); err != nil {
		t.Fatalf("AddBlacklist() error = %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
		t.Fatalf("chat delete failed: %v", err)
	}

	var count int64
	if err := db.DB.Model(&models.BlacklistSettings{}).Where("chat_id = ?", chatID).Count(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("blacklist rows after chat delete = %d, want 0", count)
	}
}

// TestConcurrentBlacklistEdits asserts that simultaneous adds, removals and
// action changes complete without lost updates or database-lock failures.
func TestConcurrentBlacklistEdits(t *testing.T) {
	skipIfNoDb(t)

	chatID := -time.Now().UnixNano()
	t.Cleanup(func() {
		_ = RemoveAllBlacklist(chatID)
	})

	if err := chats.EnsureChatInDb(chatID, ""); err != nil {
		t.Fatalf("EnsureChatInDb() error = %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers*3)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			word := "shared"
			if i%2 == 1 {
				word = "unique-" + time.Duration(i).String()
			}
			if err := AddBlacklist(chatID, word); err != nil {
				errs <- err
			}
			if err := SetBlacklistAction(chatID, "mute"); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent blacklist edit failed: %v", err)
	}

	settings := GetBlacklistSettings(chatID)
	shared := 0
	for _, s := range settings {
		if s.Word == "shared" {
			shared++
		}
		if s.Action != "mute" {
			t.Fatalf("Action = %q for word %q, want %q", s.Action, s.Word, "mute")
		}
	}
	if shared != 1 {
		t.Fatalf("duplicate 'shared' rows = %d, want exactly 1", shared)
	}
}
