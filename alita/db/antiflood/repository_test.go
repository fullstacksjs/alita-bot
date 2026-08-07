package antiflood

import (
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

// sqliteFilePath returns the on-disk path backing the test database.
func sqliteFilePath(t *testing.T) string {
	t.Helper()

	var rows []struct {
		Seq  int
		Name string
		File string
	}
	if err := db.DB.Raw("PRAGMA database_list").Scan(&rows).Error; err != nil {
		t.Fatalf("PRAGMA database_list failed: %v", err)
	}
	for _, row := range rows {
		if row.Name == "main" && row.File != "" {
			return row.File
		}
	}
	t.Skip("test database is not file-backed")
	return ""
}

// TestFloodSettingsPersistAcrossReopen proves the threshold, mode and deletion
// flag survive a full close/reopen of the SQLite file.
func TestFloodSettingsPersistAcrossReopen(t *testing.T) {
	skipIfNoDb(t)

	path := sqliteFilePath(t)
	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if err := SetFlood(chatID, 7); err != nil {
		t.Fatalf("SetFlood() error = %v", err)
	}
	if err := SetFloodMode(chatID, "kick"); err != nil {
		t.Fatalf("SetFloodMode() error = %v", err)
	}
	if err := SetFloodMsgDel(chatID, true); err != nil {
		t.Fatalf("SetFloodMsgDel() error = %v", err)
	}

	reopened, err := gorm.Open(sqlite.Open(db.FormatSQLiteDSN(path)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := reopened.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	var settings models.AntifloodSettings
	if err := reopened.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("reopened read failed: %v", err)
	}
	if settings.Limit != 7 {
		t.Fatalf("Limit = %d, want 7", settings.Limit)
	}
	if settings.Action != "kick" {
		t.Fatalf("Action = %q, want %q", settings.Action, "kick")
	}
	if !settings.DeleteAntifloodMessage {
		t.Fatal("DeleteAntifloodMessage = false, want true")
	}
}

// TestConcurrentFloodSettingChanges asserts that simultaneous setting writes
// neither lose an update nor surface a database-lock failure.
func TestConcurrentFloodSettingChanges(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// Seed the row so every worker takes the conflict path.
	if err := SetFlood(chatID, 1); err != nil {
		t.Fatalf("SetFlood() seed error = %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers*3)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := SetFlood(chatID, 2+i); err != nil {
				errs <- err
			}
			if err := SetFloodMode(chatID, "ban"); err != nil {
				errs <- err
			}
			if err := SetFloodMsgDel(chatID, i%2 == 0); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent setting change failed: %v", err)
	}

	var count int64
	if err := db.DB.Model(&models.AntifloodSettings{}).Where("chat_id = ?", chatID).Count(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("antiflood rows = %d, want exactly 1", count)
	}

	// The last writer of each column wins; no worker may leave a partial row.
	var settings models.AntifloodSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("read-back failed: %v", err)
	}
	if settings.Action != "ban" {
		t.Fatalf("Action = %q, want %q", settings.Action, "ban")
	}
	if settings.Limit < 2 || settings.Limit > workers+1 {
		t.Fatalf("Limit = %d, want a value written by one of the workers", settings.Limit)
	}
}

func skipIfNoDb(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
}

func TestSetFloodMsgDelZeroValueBoolean(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()

	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// Set to true first
	if err := SetFloodMsgDel(chatID, true); err != nil {
		t.Fatalf("SetFloodMsgDel(true) failed: %v", err)
	}

	var settings models.AntifloodSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("expected record to exist after SetFloodMsgDel(true), got error: %v", err)
	}
	if !settings.DeleteAntifloodMessage {
		t.Fatalf("expected DeleteAntifloodMessage=true, got false")
	}

	// Now set to false — this was the bug: zero value was silently skipped
	if err := SetFloodMsgDel(chatID, false); err != nil {
		t.Fatalf("SetFloodMsgDel(false) failed: %v", err)
	}

	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("query error after SetFloodMsgDel(false): %v", err)
	}
	if settings.DeleteAntifloodMessage {
		t.Fatalf("expected DeleteAntifloodMessage=false after update, got true")
	}
}

func TestSetFloodZeroValueLimit(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()

	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// Set limit to 5 (enable flood detection)
	if err := SetFlood(chatID, 5); err != nil {
		t.Fatalf("SetFlood(5) failed: %v", err)
	}

	var settings models.AntifloodSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("expected record after SetFlood(5), got error: %v", err)
	}
	if settings.Limit != 5 {
		t.Fatalf("expected Limit=5, got %d", settings.Limit)
	}

	// Set limit to 0 (disable) — this was the bug: zero value was silently skipped
	if err := SetFlood(chatID, 0); err != nil {
		t.Fatalf("SetFlood(0) failed: %v", err)
	}

	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("query error after SetFlood(0): %v", err)
	}
	if settings.Limit != 0 {
		t.Fatalf("expected Limit=0 after disabling flood, got %d", settings.Limit)
	}
}

func TestSetFloodMsgDelCreatesRecord(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()

	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// First-time call on a fresh chat should create a record
	if err := SetFloodMsgDel(chatID, true); err != nil {
		t.Fatalf("SetFloodMsgDel(true) failed: %v", err)
	}

	var settings models.AntifloodSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("expected record to be created, got error: %v", err)
	}
	if !settings.DeleteAntifloodMessage {
		t.Fatalf("expected DeleteAntifloodMessage=true, got false")
	}
}

func TestSetFloodMode(t *testing.T) {
	skipIfNoDb(t)

	t.Run("creates record with valid mode", func(t *testing.T) {
		chatID := time.Now().UnixNano()
		t.Cleanup(func() {
			if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		if err := SetFloodMode(chatID, "ban"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		var settings models.AntifloodSettings
		if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
			t.Fatalf("expected record to exist, got error: %v", err)
		}
		if settings.Action != "ban" {
			t.Fatalf("expected action=ban, got %s", settings.Action)
		}
	})

	t.Run("updates existing record", func(t *testing.T) {
		chatID := time.Now().UnixNano()
		t.Cleanup(func() {
			if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		if err := SetFloodMode(chatID, "kick"); err != nil {
			t.Fatalf("initial SetFloodMode failed: %v", err)
		}
		if err := SetFloodMode(chatID, "warn"); err != nil {
			t.Fatalf("update SetFloodMode failed: %v", err)
		}

		var settings models.AntifloodSettings
		if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
			t.Fatalf("query error: %v", err)
		}
		if settings.Action != "warn" {
			t.Fatalf("expected action=warn, got %s", settings.Action)
		}
	})

	t.Run("no-op when mode matches existing", func(t *testing.T) {
		chatID := time.Now().UnixNano()
		t.Cleanup(func() {
			if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		if err := SetFloodMode(chatID, "tban"); err != nil {
			t.Fatalf("initial SetFloodMode failed: %v", err)
		}
		if err := SetFloodMode(chatID, "tban"); err != nil {
			t.Fatalf("no-op SetFloodMode failed: %v", err)
		}

		var settings models.AntifloodSettings
		if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
			t.Fatalf("query error: %v", err)
		}
		if settings.Action != "tban" {
			t.Fatalf("expected action=tban, got %s", settings.Action)
		}
	})

	t.Run("default mode no-op does not create record", func(t *testing.T) {
		chatID := time.Now().UnixNano()
		t.Cleanup(func() {
			if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		// Default action is "mute"; on a fresh chat this should be a no-op
		if err := SetFloodMode(chatID, "mute"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		var count int64
		if err := db.DB.Model(&models.AntifloodSettings{}).Where("chat_id = ?", chatID).Count(&count).Error; err != nil {
			t.Fatalf("count query failed: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no record for default mode no-op, got count=%d", count)
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		chatID := time.Now().UnixNano()
		t.Cleanup(func() {
			if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		err := SetFloodMode(chatID, "invalid")
		if err == nil {
			t.Fatalf("expected error for invalid mode, got nil")
		}
	})
}

func TestLoadAntifloodStats(t *testing.T) {
	skipIfNoDb(t)

	t.Run("empty table returns zero", func(t *testing.T) {
		// Ensure table is empty for this assertion
		if err := db.DB.Where("1 = 1").Delete(&models.AntifloodSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}

		count := LoadAntifloodStats()
		if count != 0 {
			t.Fatalf("expected 0 for empty table, got %d", count)
		}
	})

	t.Run("counts only enabled chats", func(t *testing.T) {
		chat1 := time.Now().UnixNano()
		chat2 := chat1 + 1
		chat3 := chat1 + 2

		t.Cleanup(func() {
			if err := db.DB.Where("chat_id IN ?", []int64{chat1, chat2, chat3}).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		// chat1: enabled (limit > 0)
		if err := SetFlood(chat1, 5); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFloodMode(chat1, "ban"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		// chat2: disabled (limit = 0) — must create record with non-zero limit first
		if err := SetFlood(chat2, 5); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFlood(chat2, 0); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFloodMode(chat2, "mute"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		// chat3: enabled (limit > 0)
		if err := SetFlood(chat3, 10); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFloodMode(chat3, "kick"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		count := LoadAntifloodStats()
		if count != 2 {
			t.Fatalf("expected 2 enabled chats, got %d", count)
		}
	})

	t.Run("all disabled returns zero", func(t *testing.T) {
		chat1 := time.Now().UnixNano()
		chat2 := chat1 + 1

		t.Cleanup(func() {
			if err := db.DB.Where("chat_id IN ?", []int64{chat1, chat2}).Delete(&models.AntifloodSettings{}).Error; err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
		})

		// Create records with non-zero limit first, then set to 0.
		// SetFlood(chat, 0) on a fresh chat is a no-op because the default limit is 0.
		if err := SetFlood(chat1, 5); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFlood(chat1, 0); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFloodMode(chat1, "mute"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		if err := SetFlood(chat2, 5); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFlood(chat2, 0); err != nil {
			t.Fatalf("SetFlood failed: %v", err)
		}
		if err := SetFloodMode(chat2, "ban"); err != nil {
			t.Fatalf("SetFloodMode failed: %v", err)
		}

		count := LoadAntifloodStats()
		if count != 0 {
			t.Fatalf("expected 0 for all disabled, got %d", count)
		}
	})
}
