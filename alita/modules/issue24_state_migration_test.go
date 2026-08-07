package modules

import (
	"context"
	"testing"

	"github.com/divkix/Alita_Robot/alita/db/backup"
	"github.com/divkix/Alita_Robot/alita/utils/ratelimit"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

func TestIssue24_FilterAndNoteOverwriteConsumeOnceAndExpire(t *testing.T) {
	state.SimulateRestart()

	// Filter overwrite token
	filterToken := "token_filter_123"
	filterData := overwriteFilter{
		overwriteBase: overwriteBase{
			ChatID:   -1001,
			UserID:   100,
			ItemName: "test_filter",
			Text:     "response",
		},
	}

	if err := setFilterOverwriteCache(filterToken, filterData); err != nil {
		t.Fatalf("setFilterOverwriteCache() error = %v", err)
	}

	// First consume succeeds
	gotFilter, err := consumeFilterOverwriteCache(filterToken)
	if err != nil || gotFilter == nil {
		t.Fatalf("consumeFilterOverwriteCache() first call error = %v, got = %v", err, gotFilter)
	}
	if gotFilter.ItemName != "test_filter" {
		t.Fatalf("gotFilter.ItemName = %q, want test_filter", gotFilter.ItemName)
	}

	// Second consume fails (consume-once)
	if _, err := consumeFilterOverwriteCache(filterToken); err == nil {
		t.Fatal("consumeFilterOverwriteCache() second call error = nil, want consume-once error")
	}

	// Note overwrite token
	noteToken := "token_note_456"
	noteData := overwriteNote{
		overwriteBase: overwriteBase{
			ChatID:   -1002,
			UserID:   200,
			ItemName: "test_note",
			Text:     "content",
		},
	}

	if err := setNoteOverwriteCache(noteToken, noteData); err != nil {
		t.Fatalf("setNoteOverwriteCache() error = %v", err)
	}

	// First consume succeeds
	gotNote, err := consumeNoteOverwriteCache(noteToken)
	if err != nil || gotNote == nil {
		t.Fatalf("consumeNoteOverwriteCache() first call error = %v, got = %v", err, gotNote)
	}

	// Second consume fails
	if _, err := consumeNoteOverwriteCache(noteToken); err == nil {
		t.Fatal("consumeNoteOverwriteCache() second call error = nil, want consume-once error")
	}

	// Verify clearing state via SimulateRestart
	tokenToClear := "token_clear_789"
	if err := setFilterOverwriteCache(tokenToClear, filterData); err != nil {
		t.Fatalf("setFilterOverwriteCache() error = %v", err)
	}

	state.SimulateRestart()

	if _, err := getFilterOverwriteCache(tokenToClear); err == nil {
		t.Fatal("getFilterOverwriteCache() after SimulateRestart error = nil, want cleared state")
	}
}

func TestIssue24_BackupConfirmationsAndRateLimitsUseTTLState(t *testing.T) {
	state.SimulateRestart()

	chatID := int64(-10099)
	bkpFormat := &backup.BackupFormat{
		ChatName: "Test Chat",
		Domains:  []string{"filters"},
	}

	// Store pending import and reset
	impToken, err := storePendingImport(chatID, bkpFormat, []string{"filters"})
	if err != nil {
		t.Fatalf("storePendingImport() error = %v", err)
	}

	resetToken, err := storePendingReset(chatID, []string{"filters"})
	if err != nil {
		t.Fatalf("storePendingReset() error = %v", err)
	}

	// Backup rate limit
	limiter := ratelimit.GetBackupRateLimiter()
	allowed, _ := limiter.AcquireExport(chatID)
	if !allowed {
		t.Fatal("AcquireExport() first call = false, want true")
	}
	allowedAgain, _ := limiter.AcquireExport(chatID)
	if allowedAgain {
		t.Fatal("AcquireExport() second call = true, want rate limited")
	}

	// Simulate restart clears confirmations and rate limits
	state.SimulateRestart()

	if _, ok := claimPendingImport(chatID, impToken); ok {
		t.Fatal("claimPendingImport() after SimulateRestart = true, want cleared")
	}
	if _, ok := claimPendingReset(chatID, resetToken); ok {
		t.Fatal("claimPendingReset() after SimulateRestart = true, want cleared")
	}
	if allowedAfterRestart, _ := limiter.AcquireExport(chatID); !allowedAfterRestart {
		t.Fatal("AcquireExport() after SimulateRestart = false, want cleared rate limit")
	}
}

func TestIssue24_DetectionCountersResetOnRestartButRaidExpiryPersisted(t *testing.T) {
	state.SimulateRestart()
	chatID := uniqueModuleChatID()

	// Track join in anti-raid
	count1, err := trackJoin(chatID, 101)
	if err != nil || count1 != 1 {
		t.Fatalf("trackJoin() = (%d, %v), want (1, nil)", count1, err)
	}

	// Enable anti-raid window (persisted in SQLite)
	enabled, err := antiRaidModule.enableRaid(chatID, 3600)
	if err != nil || !enabled {
		t.Fatalf("enableRaid() = (%v, %v), want (true, nil)", enabled, err)
	}
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chatID)
	})

	// Simulate restart
	state.SimulateRestart()

	// Active anti-raid window remains active (loaded from DB)
	if !antiRaidModule.isRaidActive(chatID) {
		t.Fatal("isRaidActive() after SimulateRestart = false, want persisted true in DB")
	}

	// Detection counter was reset in TTL state
	if entries, ok := state.Get[[]joinEntry](context.Background(), joinsKey(chatID)); ok && len(entries) > 0 {
		t.Fatalf("join tracking entries after SimulateRestart = %v, want cleared", entries)
	}
}
