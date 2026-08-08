package antiraid

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

func skipIfNoDb(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
}

func TestSetRaidTime(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if err := SetRaidTime(chatID, 10800); err != nil {
		t.Fatalf("SetRaidTime failed: %v", err)
	}

	var settings models.AntiRaidSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("expected record, got error: %v", err)
	}
	if settings.RaidTime != 10800 {
		t.Fatalf("expected RaidTime=10800, got %d", settings.RaidTime)
	}
}

func TestSetRaidTimeZeroValue(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// Set to non-zero first, then set to 0 (zero value must persist)
	if err := SetRaidTime(chatID, 10800); err != nil {
		t.Fatalf("SetRaidTime(10800) failed: %v", err)
	}
	if err := SetRaidTime(chatID, 0); err != nil {
		t.Fatalf("SetRaidTime(0) failed: %v", err)
	}

	var settings models.AntiRaidSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("query error: %v", err)
	}
	if settings.RaidTime != 0 {
		t.Fatalf("expected RaidTime=0 after update, got %d", settings.RaidTime)
	}
}

func TestSetRaidTimeNoOp(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// First call creates record
	if err := SetRaidTime(chatID, 7200); err != nil {
		t.Fatalf("SetRaidTime(7200) failed: %v", err)
	}
	// Second call with same value should be no-op but not error
	if err := SetRaidTime(chatID, 7200); err != nil {
		t.Fatalf("no-op SetRaidTime(7200) failed: %v", err)
	}

	var settings models.AntiRaidSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("query error: %v", err)
	}
	if settings.RaidTime != 7200 {
		t.Fatalf("expected RaidTime=7200, got %d", settings.RaidTime)
	}
}

func TestSetRaidActionTime(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// Use non-default value (default is 3600) to trigger actual DB write
	if err := SetRaidActionTime(chatID, 1800); err != nil {
		t.Fatalf("SetRaidActionTime failed: %v", err)
	}

	var settings models.AntiRaidSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("expected record, got error: %v", err)
	}
	if settings.RaidActionTime != 1800 {
		t.Fatalf("expected RaidActionTime=1800, got %d", settings.RaidActionTime)
	}
}

func TestSetAutoAntiRaidThreshold(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if err := SetAutoAntiRaidThreshold(chatID, 5); err != nil {
		t.Fatalf("SetAutoAntiRaidThreshold failed: %v", err)
	}

	var settings models.AntiRaidSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("expected record, got error: %v", err)
	}
	if settings.AutoAntiRaidThreshold != 5 {
		t.Fatalf("expected AutoAntiRaidThreshold=5, got %d", settings.AutoAntiRaidThreshold)
	}
}

func TestDefaultAntiRaidSettings(t *testing.T) {
	t.Parallel()

	settings := defaultAntiRaidSettings(-100123)
	if settings.ChatID != -100123 {
		t.Fatalf("ChatID = %d, want -100123", settings.ChatID)
	}
	if settings.RaidTime != 21600 {
		t.Fatalf("RaidTime = %d, want 21600", settings.RaidTime)
	}
	if settings.RaidActionTime != 3600 {
		t.Fatalf("RaidActionTime = %d, want 3600", settings.RaidActionTime)
	}
	if settings.AutoAntiRaidThreshold != 0 {
		t.Fatalf("AutoAntiRaidThreshold = %d, want 0", settings.AutoAntiRaidThreshold)
	}
}

func TestAntiRaidSettersRejectNegativeValues(t *testing.T) {
	t.Parallel()

	chatID := time.Now().UnixNano()
	tests := []struct {
		name               string
		call               func() error
		expectedErrMessage string
	}{
		{name: "raid time", call: func() error { return SetRaidTime(chatID, -1) }, expectedErrMessage: "raid time must be non-negative"},
		{name: "raid action time", call: func() error { return SetRaidActionTime(chatID, -1) }, expectedErrMessage: "raid action time must be non-negative"},
		{name: "auto threshold", call: func() error { return SetAutoAntiRaidThreshold(chatID, -1) }, expectedErrMessage: "threshold must be non-negative"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected negative value error, got nil")
			}
			if !strings.Contains(err.Error(), tc.expectedErrMessage) {
				t.Fatalf("error = %v, want substring %q", err, tc.expectedErrMessage)
			}
		})
	}
}

func TestSetAutoAntiRaidThresholdZeroValue(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if err := SetAutoAntiRaidThreshold(chatID, 10); err != nil {
		t.Fatalf("SetAutoAntiRaidThreshold(10) failed: %v", err)
	}
	if err := SetAutoAntiRaidThreshold(chatID, 0); err != nil {
		t.Fatalf("SetAutoAntiRaidThreshold(0) failed: %v", err)
	}

	var settings models.AntiRaidSettings
	if err := db.DB.Where("chat_id = ?", chatID).First(&settings).Error; err != nil {
		t.Fatalf("query error: %v", err)
	}
	if settings.AutoAntiRaidThreshold != 0 {
		t.Fatalf("expected AutoAntiRaidThreshold=0 after update, got %d", settings.AutoAntiRaidThreshold)
	}
}

func TestGetAntiRaidSettingsDefault(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	// No record created, should return defaults

	settings := GetAntiRaidSettings(chatID)
	if settings == nil {
		t.Fatal("expected default settings, got nil")
	}
	if settings.ChatID != chatID {
		t.Fatalf("expected ChatID=%d, got %d", chatID, settings.ChatID)
	}
	if settings.RaidTime != 21600 {
		t.Fatalf("expected default RaidTime=21600, got %d", settings.RaidTime)
	}
	if settings.RaidActionTime != 3600 {
		t.Fatalf("expected default RaidActionTime=3600, got %d", settings.RaidActionTime)
	}
	if settings.AutoAntiRaidThreshold != 0 {
		t.Fatalf("expected default AutoAntiRaidThreshold=0, got %d", settings.AutoAntiRaidThreshold)
	}
}

func TestGetAntiRaidSettingsWithRecord(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if err := SetRaidTime(chatID, 7200); err != nil {
		t.Fatalf("setup SetRaidTime failed: %v", err)
	}
	if err := SetRaidActionTime(chatID, 1800); err != nil {
		t.Fatalf("setup SetRaidActionTime failed: %v", err)
	}
	if err := SetAutoAntiRaidThreshold(chatID, 3); err != nil {
		t.Fatalf("setup SetAutoAntiRaidThreshold failed: %v", err)
	}

	settings := GetAntiRaidSettings(chatID)
	if settings == nil {
		t.Fatal("expected settings, got nil")
	}
	if settings.RaidTime != 7200 {
		t.Fatalf("expected RaidTime=7200, got %d", settings.RaidTime)
	}
	if settings.RaidActionTime != 1800 {
		t.Fatalf("expected RaidActionTime=1800, got %d", settings.RaidActionTime)
	}
	if settings.AutoAntiRaidThreshold != 3 {
		t.Fatalf("expected AutoAntiRaidThreshold=3, got %d", settings.AutoAntiRaidThreshold)
	}
}

func TestSetAntiRaidThresholdNegativeRejection(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	err := SetAutoAntiRaidThreshold(chatID, -1)
	if err == nil {
		t.Fatal("expected error for negative threshold, got nil")
	}
}

func TestAntiRaidSettingsCacheInvalidation(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	// Create initial record via setter (populates cache)
	if err := SetRaidTime(chatID, 3600); err != nil {
		t.Fatalf("SetRaidTime failed: %v", err)
	}

	// Populate cache with the initial value
	first := GetAntiRaidSettings(chatID)
	if first.RaidTime != 3600 {
		t.Fatalf("expected cached RaidTime=3600, got %d", first.RaidTime)
	}

	// Direct DB update to simulate external change; the cache is now stale
	if err := db.DB.Model(&models.AntiRaidSettings{}).Where("chat_id = ?", chatID).Update("raid_time", 10800).Error; err != nil {
		t.Fatalf("direct DB update failed: %v", err)
	}

	// Stale cached value should still reflect 3600
	stale := GetAntiRaidSettings(chatID)
	if stale.RaidTime != 3600 {
		t.Fatalf("expected stale cached RaidTime=3600, got %d", stale.RaidTime)
	}

	// Setter should invalidate cache and persist the new value
	if err := SetRaidTime(chatID, 10800); err != nil {
		t.Fatalf("SetRaidTime(10800) failed: %v", err)
	}

	// After cache invalidation, read should reflect the DB update (10800)
	fresh := GetAntiRaidSettings(chatID)
	if fresh.RaidTime != 10800 {
		t.Fatalf("expected RaidTime=10800 after cache invalidation, got %d", fresh.RaidTime)
	}
}

func TestRaidStateLifecycle(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if state := GetRaidState(chatID); state.Active {
		t.Fatalf("GetRaidState() = %+v, want inactive for a fresh chat", state)
	}

	enabled, err := EnableRaid(chatID, 3600)
	if err != nil || !enabled {
		t.Fatalf("EnableRaid() = (%v, %v), want (true, nil)", enabled, err)
	}
	state := GetRaidState(chatID)
	if !state.Active {
		t.Fatal("GetRaidState() inactive right after EnableRaid()")
	}
	if state.StartedAt == 0 {
		t.Fatal("GetRaidState().StartedAt = 0, want the raid start time")
	}

	// Enabling again must not reset the deadline of the running raid.
	firstExpiry := state.ExpiresAt
	enabled, err = EnableRaid(chatID, 7200)
	if err != nil {
		t.Fatalf("EnableRaid(active) error = %v", err)
	}
	if enabled {
		t.Fatal("EnableRaid(active) = true, want false for an already-active raid")
	}
	if got := GetRaidState(chatID).ExpiresAt; got != firstExpiry {
		t.Fatalf("ExpiresAt = %d after redundant enable, want %d", got, firstExpiry)
	}

	// Extending an active raid moves the deadline forward.
	if err := SetRaidDuration(chatID, 7200); err != nil {
		t.Fatalf("SetRaidDuration() error = %v", err)
	}
	if got := GetRaidState(chatID).ExpiresAt; got <= firstExpiry {
		t.Fatalf("ExpiresAt = %d after extension, want > %d", got, firstExpiry)
	}

	disabled, err := DisableRaid(chatID)
	if err != nil || !disabled {
		t.Fatalf("DisableRaid() = (%v, %v), want (true, nil)", disabled, err)
	}
	if state := GetRaidState(chatID); state.Active {
		t.Fatalf("GetRaidState() = %+v, want inactive after DisableRaid()", state)
	}

	disabled, err = DisableRaid(chatID)
	if err != nil {
		t.Fatalf("DisableRaid(inactive) error = %v", err)
	}
	if disabled {
		t.Fatal("DisableRaid(inactive) = true, want false")
	}
}

// TestRaidStatePreservesConfiguration checks that opening and closing a raid
// window leaves the chat's configured durations and threshold untouched.
func TestRaidStatePreservesConfiguration(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if err := SetRaidTime(chatID, 7200); err != nil {
		t.Fatalf("SetRaidTime() error = %v", err)
	}
	if err := SetRaidActionTime(chatID, 1800); err != nil {
		t.Fatalf("SetRaidActionTime() error = %v", err)
	}
	if err := SetAutoAntiRaidThreshold(chatID, 4); err != nil {
		t.Fatalf("SetAutoAntiRaidThreshold() error = %v", err)
	}

	if _, err := EnableRaid(chatID, 600); err != nil {
		t.Fatalf("EnableRaid() error = %v", err)
	}
	// A configuration change during an active raid must not close the window.
	if err := SetRaidTime(chatID, 5400); err != nil {
		t.Fatalf("SetRaidTime(during raid) error = %v", err)
	}
	if !GetRaidState(chatID).Active {
		t.Fatal("configuration change closed the active raid window")
	}
	if _, err := DisableRaid(chatID); err != nil {
		t.Fatalf("DisableRaid() error = %v", err)
	}

	settings := GetAntiRaidSettings(chatID)
	if settings.RaidTime != 5400 {
		t.Fatalf("RaidTime = %d, want 5400", settings.RaidTime)
	}
	if settings.RaidActionTime != 1800 {
		t.Fatalf("RaidActionTime = %d, want 1800", settings.RaidActionTime)
	}
	if settings.AutoAntiRaidThreshold != 4 {
		t.Fatalf("AutoAntiRaidThreshold = %d, want 4", settings.AutoAntiRaidThreshold)
	}
}

func TestEnableRaidRejectsOutOfRangeDurations(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	for _, duration := range []int{0, -1, MaxRaidDuration + 1} {
		if _, err := EnableRaid(chatID, duration); err == nil {
			t.Fatalf("EnableRaid(%d) = nil error, want rejection", duration)
		}
		if err := SetRaidDuration(chatID, duration); err == nil {
			t.Fatalf("SetRaidDuration(%d) = nil error, want rejection", duration)
		}
	}
}

// TestConcurrentEnableRaidHasSingleWinner guards the raid window against lost
// updates: only one caller may open it, and no caller may see a lock failure.
func TestConcurrentEnableRaidHasSingleWinner(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	const workers = 12
	var winners atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enabled, err := EnableRaid(chatID, 3600)
			if err != nil {
				errs <- err
				return
			}
			if enabled {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("EnableRaid() error = %v", err)
	}
	if got := winners.Load(); got != 1 {
		t.Fatalf("EnableRaid() winners = %d, want 1", got)
	}
	if !GetRaidState(chatID).Active {
		t.Fatal("winning EnableRaid() state cannot be read back")
	}
}

// TestExpireRaidsClearsOnlyElapsedWindows also covers the restart path: the
// windows are read straight from storage, with no in-process state involved.
func TestExpireRaidsClearsOnlyElapsedWindows(t *testing.T) {
	skipIfNoDb(t)

	elapsedChatID := time.Now().UnixNano()
	liveChatID := elapsedChatID + 1
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id IN ?", []int64{elapsedChatID, liveChatID}).
			Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if _, err := EnableRaid(elapsedChatID, 1); err != nil {
		t.Fatalf("EnableRaid(elapsed) error = %v", err)
	}
	if _, err := EnableRaid(liveChatID, 3600); err != nil {
		t.Fatalf("EnableRaid(live) error = %v", err)
	}

	expired, err := ExpireRaids(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("ExpireRaids() error = %v", err)
	}
	if len(expired) != 1 || expired[0] != elapsedChatID {
		t.Fatalf("ExpireRaids() = %v, want [%d]", expired, elapsedChatID)
	}
	if GetRaidState(elapsedChatID).ExpiresAt != 0 {
		t.Fatal("elapsed raid window was not cleared")
	}
	if !GetRaidState(liveChatID).Active {
		t.Fatal("ExpireRaids() closed a raid that is still within its window")
	}

	// A second sweep is idempotent.
	expired, err = ExpireRaids(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatalf("ExpireRaids(second) error = %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("ExpireRaids(second) = %v, want no chats", expired)
	}
}

// TestRaidWindowSurvivesReopen proves the expiry worker can recover an
// in-flight raid after a process restart.
func TestRaidWindowSurvivesReopen(t *testing.T) {
	skipIfNoDb(t)

	var rows []struct {
		Seq  int
		Name string
		File string
	}
	if err := db.DB.Raw("PRAGMA database_list").Scan(&rows).Error; err != nil {
		t.Fatalf("PRAGMA database_list failed: %v", err)
	}
	path := ""
	for _, row := range rows {
		if row.Name == "main" && row.File != "" {
			path = row.File
		}
	}
	if path == "" {
		t.Skip("test database is not file-backed")
	}

	chatID := time.Now().UnixNano()
	t.Cleanup(func() {
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	if _, err := EnableRaid(chatID, 3600); err != nil {
		t.Fatalf("EnableRaid() error = %v", err)
	}
	expected := GetRaidState(chatID)

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

	original := db.DB
	db.DB = reopened
	t.Cleanup(func() { db.DB = original })

	recovered := GetRaidState(chatID)
	if !recovered.Active {
		t.Fatal("raid window did not survive the reopen")
	}
	if recovered.ExpiresAt != expected.ExpiresAt {
		t.Fatalf("recovered ExpiresAt = %d, want %d", recovered.ExpiresAt, expected.ExpiresAt)
	}
}
