package modules

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/antiraid"
	"github.com/divkix/Alita_Robot/alita/db/approvals"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantSec int
		wantOk  bool
	}{
		{"minutes", "30m", 30 * 60, true},
		{"hours", "2h", 2 * 60 * 60, true},
		{"days", "1d", 24 * 60 * 60, true},
		{"weeks", "1w", 7 * 24 * 60 * 60, true},
		{"raw seconds", "3600", 3600, true},
		{"bare single digit", "5", 5, true},
		{"bare 30 seconds", "30", 30, true},
		{"s suffix", "30s", 30, true},
		{"5m suffix", "5m", 300, true},
		{"2h suffix", "2h", 7200, true},
		{"1d suffix", "1d", 86400, true},
		{"1w suffix", "1w", 604800, true},
		{"empty", "", 0, false},
		{"garbage", "abc", 0, false},
		{"unknown unit", "30x", 0, false},
		{"negative bare", "-5", 0, false},
		{"negative minutes", "-5m", 0, false},
		{"overflowing weeks", "9223372036854775807w", 0, false},
		{"above operational maximum", "367d", 0, false},
		{"operational maximum", "366d", 366 * 24 * 60 * 60, true},
		{"uppercase", "1H", 3600, true},
		{"whitespace", "  5m  ", 5 * 60, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseDuration(tc.input)
			if got != tc.wantSec || ok != tc.wantOk {
				t.Errorf("parseDuration(%q) = (%d, %v), want (%d, %v)", tc.input, got, ok, tc.wantSec, tc.wantOk)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    int
		expected string
	}{
		{60, "1m"},
		{3600, "1h"},
		{86400, "1d"},
		{604800, "1w"},
		{30, "30s"},
		{7200, "2h"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.expected, func(t *testing.T) {
			t.Parallel()
			got := formatDuration(tc.input)
			if got != tc.expected {
				t.Errorf("formatDuration(%d) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestAntiRaidKeysAndNoCacheFallbacks(t *testing.T) {

	chatID := int64(-1001234567890)
	if got := joinsKey(chatID); got != "alita:antiraid:joins:-1001234567890" {
		t.Fatalf("joinsKey() = %q", got)
	}

	count, err := trackJoin(chatID, 42)
	if err != nil {
		t.Fatalf("trackJoin() error = %v, want nil", err)
	}
	if count != 1 {
		t.Fatalf("trackJoin() count = %d, want 1", count)
	}

	clearJoinTracking(chatID)

	// Raid state lives in SQLite, so it stays readable while the cache is down.
	state := getRaidState(chatID)
	if state == nil {
		t.Fatal("getRaidState() = nil, want inactive state")
	}
	if state.Active {
		t.Fatalf("getRaidState() Active = true, want false")
	}
}

func TestStopAntiRaidExpiryPollerCancelsExistingContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	antiRaidCtx = ctx
	antiRaidCancel = cancel
	t.Cleanup(func() {
		antiRaidCancel = nil
		antiRaidCtx = nil
	})

	StopAntiRaidExpiryPoller()
	if antiRaidCancel != nil {
		t.Fatal("antiRaidCancel was not cleared")
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("anti-raid context was not cancelled")
	}
}

func TestStartAntiRaidExpiryPollerSkipsWhenDatabaseUnavailable(t *testing.T) {
	antiRaidCancel = nil
	antiRaidCtx = nil
	originalDB := db.DB
	db.DB = nil
	t.Cleanup(func() {
		db.DB = originalDB
		StopAntiRaidExpiryPoller()
		antiRaidCtx = nil
	})

	StartAntiRaidExpiryPoller()
	if antiRaidCancel != nil {
		t.Fatal("StartAntiRaidExpiryPoller created cancel func without a database")
	}
}

func TestStartAntiRaidExpiryPollerRunsWithoutRedis(t *testing.T) {
	antiRaidCancel = nil
	antiRaidCtx = nil
	t.Cleanup(func() {
		StopAntiRaidExpiryPoller()
		antiRaidCtx = nil
	})

	// The raid window is persisted in SQLite.
	StartAntiRaidExpiryPoller()
	if antiRaidCancel == nil {
		t.Fatal("StartAntiRaidExpiryPoller did not start")
	}
}

func TestAntiRaidPollerReturnsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		antiRaidModule.expiryPoller(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expiryPoller did not return after context cancellation")
	}
}

func TestAntiRaidCheckExpiredRaidsNoDatabaseIsNoop(t *testing.T) {
	originalDB := db.DB
	db.DB = nil
	t.Cleanup(func() { db.DB = originalDB })

	antiRaidModule.checkExpiredRaids(context.Background())
}

func TestAntiRaidStateMachine(t *testing.T) {
	chatID := uniqueModuleChatID()

	// Initial state
	if antiRaidModule.isRaidActive(chatID) {
		t.Fatal("expected raid to be inactive initially")
	}

	// Enable
	antiRaidModule.enableRaid(chatID, 3600)
	if !antiRaidModule.isRaidActive(chatID) {
		t.Fatal("expected raid to be active after enable")
	}

	// Disable
	disabled, err := antiRaidModule.disableRaid(chatID)
	if err != nil {
		t.Fatalf("disableRaid() error = %v", err)
	}
	if !disabled {
		t.Fatal("expected disableRaid to return true for active raid")
	}
	if antiRaidModule.isRaidActive(chatID) {
		t.Fatal("expected raid to be inactive after disable")
	}

	// Disable when already disabled
	disabled, err = antiRaidModule.disableRaid(chatID)
	if err != nil {
		t.Fatalf("disableRaid(inactive) error = %v", err)
	}
	if disabled {
		t.Fatal("expected disableRaid to return false for already-inactive raid")
	}
}

func TestAntiRaidConcurrentEnableHasSingleWinner(t *testing.T) {
	chatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chatID)
	})
	start := make(chan struct{})
	var winners atomic.Int32
	var workers sync.WaitGroup
	errs := make(chan error, 16)

	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			enabled, err := antiRaidModule.enableRaid(chatID, 3600)
			if err != nil {
				errs <- err
				return
			}
			if enabled {
				winners.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("enableRaid() error = %v", err)
	}
	if got := winners.Load(); got != 1 {
		t.Fatalf("enableRaid() winners = %d, want 1", got)
	}
	if !antiRaidModule.isRaidActive(chatID) {
		t.Fatal("winning enableRaid() state cannot be read back")
	}
}

// seedRaidWindow writes a raid window straight into SQLite so tests can stage
// state that a previous process would have left behind.
func seedRaidWindow(t *testing.T, chatID int64, startedAt, activeUntil time.Time) {
	t.Helper()

	if err := antiraid.SetRaidTime(chatID, 21600); err != nil {
		t.Fatalf("seedRaidWindow: SetRaidTime error = %v", err)
	}
	if err := db.DB.Model(&models.AntiRaidSettings{}).
		Where("chat_id = ?", chatID).
		Updates(map[string]any{
			"raid_started_at":   startedAt,
			"raid_active_until": activeUntil,
		}).Error; err != nil {
		t.Fatalf("seedRaidWindow: update error = %v", err)
	}
}

func TestAntiRaidStaleExpiryCannotClearFreshState(t *testing.T) {
	freshChatID := uniqueModuleChatID()
	staleChatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(freshChatID)
		_, _ = antiRaidModule.disableRaid(staleChatID)
	})

	seedRaidWindow(t, staleChatID, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	if enabled, err := antiRaidModule.enableRaid(freshChatID, 3600); err != nil || !enabled {
		t.Fatalf("enableRaid(fresh) = (%v, %v), want (true, nil)", enabled, err)
	}
	freshExpiry := getRaidState(freshChatID).ExpiresAt

	antiRaidModule.checkExpiredRaids(context.Background())

	if got := getRaidState(freshChatID); !got.Active || got.ExpiresAt != freshExpiry {
		t.Fatalf("fresh raid after expiry sweep = %+v, want active with expiry %d", got, freshExpiry)
	}
	if got := getRaidState(staleChatID); got.Active || got.ExpiresAt != 0 {
		t.Fatalf("stale raid after expiry sweep = %+v, want cleared", got)
	}
}

// TestAntiRaidExpiryPollerRecoversRaidFromStorage covers the restart path: the
// window was opened by an earlier process and only exists in SQLite.
func TestAntiRaidExpiryPollerRecoversRaidFromStorage(t *testing.T) {
	activeChatID := uniqueModuleChatID()
	expiredChatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(activeChatID)
		_, _ = antiRaidModule.disableRaid(expiredChatID)
	})

	seedRaidWindow(t, activeChatID, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	seedRaidWindow(t, expiredChatID, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Minute))

	// A restarted process sees the still-open raid without any cache priming.
	if !antiRaidModule.isRaidActive(activeChatID) {
		t.Fatal("isRaidActive() = false for a raid recovered from storage")
	}
	if antiRaidModule.isRaidActive(expiredChatID) {
		t.Fatal("isRaidActive() = true for a raid whose window already closed")
	}

	antiRaidModule.checkExpiredRaids(context.Background())

	if !antiRaidModule.isRaidActive(activeChatID) {
		t.Fatal("expiry sweep closed a raid that is still within its window")
	}
	if antiRaidModule.isRaidActive(expiredChatID) {
		t.Fatal("expiry sweep left an elapsed raid open")
	}
}

func TestAntiRaidAutoExpiry(t *testing.T) {
	chatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chatID)
	})

	if _, err := antiRaidModule.enableRaid(chatID, 1); err != nil { // 1 second
		t.Fatalf("enableRaid() error = %v", err)
	}
	if !antiRaidModule.isRaidActive(chatID) {
		t.Fatal("expected raid active immediately")
	}

	time.Sleep(2 * time.Second)
	if antiRaidModule.isRaidActive(chatID) {
		t.Fatal("expected raid expired after 1s duration")
	}
}

func TestAntiRaidExtend(t *testing.T) {
	chatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chatID)
	})

	if _, err := antiRaidModule.enableRaid(chatID, 3600); err != nil {
		t.Fatalf("enableRaid() error = %v", err)
	}
	originalExpiry := getRaidState(chatID).ExpiresAt

	if err := antiRaidModule.setRaidDuration(chatID, 7200); err != nil {
		t.Fatalf("setRaidDuration() error = %v", err)
	}

	extended := getRaidState(chatID)
	if !extended.Active {
		t.Fatal("setRaidDuration() left the raid inactive")
	}
	if extended.ExpiresAt <= originalExpiry {
		t.Fatalf("expected extended expiry > original, got %d vs %d", extended.ExpiresAt, originalExpiry)
	}
}

func TestAntiRaidExpiredStoredStateReadsInactive(t *testing.T) {
	chatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chatID)
	})

	seedRaidWindow(t, chatID, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	if antiRaidModule.isRaidActive(chatID) {
		t.Fatal("isRaidActive() = true for expired stored state")
	}
	disabled, err := antiRaidModule.disableRaid(chatID)
	if err != nil {
		t.Fatalf("disableRaid(expired) error = %v", err)
	}
	if disabled {
		t.Fatal("disableRaid() = true for already expired inactive state")
	}
}

func TestAntiRaidCommandShowsStatusAndTogglesState(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chat.Id)
	})

	statusCtx := newModuleMessageContext(bot, chat, user, "/antiraid")
	if err := antiRaidModule.antiraid(bot, statusCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(status) error = %v, want EndGroups", err)
	}

	onCtx := newModuleMessageContext(bot, chat, user, "/antiraid on")
	if err := antiRaidModule.antiraid(bot, onCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(on) error = %v, want EndGroups", err)
	}
	if !antiRaidModule.isRaidActive(chat.Id) {
		t.Fatal("raid was not activated by /antiraid on")
	}

	durationCtx := newModuleMessageContext(bot, chat, user, "/antiraid 45m")
	if err := antiRaidModule.antiraid(bot, durationCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(duration) error = %v, want EndGroups", err)
	}
	if st := getRaidState(chat.Id); st.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("duration update produced expired state: %+v", st)
	}

	offCtx := newModuleMessageContext(bot, chat, user, "/antiraid off")
	if err := antiRaidModule.antiraid(bot, offCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(off) error = %v, want EndGroups", err)
	}
	if antiRaidModule.isRaidActive(chat.Id) {
		t.Fatal("raid stayed active after /antiraid off")
	}
}

func TestAntiRaidCommandHandlesInvalidAndNoopBranches(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chat.Id)
	})

	offInactiveCtx := newModuleMessageContext(bot, chat, user, "/antiraid off")
	if err := antiRaidModule.antiraid(bot, offInactiveCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(off inactive) error = %v, want EndGroups", err)
	}

	invalidCtx := newModuleMessageContext(bot, chat, user, "/antiraid nope")
	if err := antiRaidModule.antiraid(bot, invalidCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(invalid duration) error = %v, want EndGroups", err)
	}

	// Zero duration must be rejected (parseDuration("0") = 0s, which would
	// enable an already-expired raid and report success misleadingly).
	zeroCtx := newModuleMessageContext(bot, chat, user, "/antiraid 0")
	if err := antiRaidModule.antiraid(bot, zeroCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(zero duration) error = %v, want EndGroups", err)
	}
	if antiRaidModule.isRaidActive(chat.Id) {
		t.Fatal("raid activated by /antiraid 0, want rejected")
	}

	onCtx := newModuleMessageContext(bot, chat, user, "/antiraid on")
	if err := antiRaidModule.antiraid(bot, onCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(on) error = %v, want EndGroups", err)
	}
	onAgainCtx := newModuleMessageContext(bot, chat, user, "/antiraid on")
	if err := antiRaidModule.antiraid(bot, onAgainCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(on already active) error = %v, want EndGroups", err)
	}

	activeStatusCtx := newModuleMessageContext(bot, chat, user, "/antiraid")
	if err := antiRaidModule.antiraid(bot, activeStatusCtx); err != ext.EndGroups {
		t.Fatalf("antiraid(active status) error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("sendMessage"); len(calls) < 5 {
		t.Fatalf("sendMessage calls = %d, want command replies", len(calls))
	}
}

func TestAntiRaidTimeCommandsPersistSettings(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	raidTimeCtx := newModuleMessageContext(bot, chat, user, "/raidtime 2h")
	if err := antiRaidModule.raidTime(bot, raidTimeCtx); err != ext.EndGroups {
		t.Fatalf("raidTime() error = %v, want EndGroups", err)
	}
	if got := antiraid.GetAntiRaidSettings(chat.Id).RaidTime; got != 2*60*60 {
		t.Fatalf("RaidTime = %d, want 7200", got)
	}

	actionTimeCtx := newModuleMessageContext(bot, chat, user, "/raidactiontime 30m")
	if err := antiRaidModule.raidActionTime(bot, actionTimeCtx); err != ext.EndGroups {
		t.Fatalf("raidActionTime() error = %v, want EndGroups", err)
	}
	if got := antiraid.GetAntiRaidSettings(chat.Id).RaidActionTime; got != 30*60 {
		t.Fatalf("RaidActionTime = %d, want 1800", got)
	}
}

func TestAntiRaidTimeCommandsValidateInputAndNoChange(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, text := range []string{
		"/raidtime",
		"/raidtime nope",
		"/raidtime 0",
		"/raidactiontime",
		"/raidactiontime nope",
		"/raidactiontime 0",
		"/raidactiontime 29s",
		"/raidactiontime 367d",
	} {
		ctx := newModuleMessageContext(bot, chat, user, text)
		if err := antiRaidModule.raidTimeSetter(bot, ctx, strings.HasPrefix(text, "/raidtime")); err != ext.EndGroups {
			t.Fatalf("raidTimeSetter(%q) error = %v, want EndGroups", text, err)
		}
	}

	if err := antiraid.SetRaidTime(chat.Id, 60); err != nil {
		t.Fatalf("SetRaidTime setup error = %v", err)
	}
	noChangeRaidCtx := newModuleMessageContext(bot, chat, user, "/raidtime 1m")
	if err := antiRaidModule.raidTime(bot, noChangeRaidCtx); err != ext.EndGroups {
		t.Fatalf("raidTime(no change) error = %v, want EndGroups", err)
	}

	if err := antiraid.SetRaidActionTime(chat.Id, 120); err != nil {
		t.Fatalf("SetRaidActionTime setup error = %v", err)
	}
	noChangeActionCtx := newModuleMessageContext(bot, chat, user, "/raidactiontime 2m")
	if err := antiRaidModule.raidActionTime(bot, noChangeActionCtx); err != ext.EndGroups {
		t.Fatalf("raidActionTime(no change) error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 10 {
		t.Fatalf("sendMessage calls = %d, want validation and no-change replies", len(calls))
	}
}

func TestAutoAntiRaidCommandPersistsThreshold(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	setCtx := newModuleMessageContext(bot, chat, user, "/autoantiraid 4")
	if err := antiRaidModule.autoAntiRaid(bot, setCtx); err != ext.EndGroups {
		t.Fatalf("autoAntiRaid(set) error = %v, want EndGroups", err)
	}
	if got := antiraid.GetAntiRaidSettings(chat.Id).AutoAntiRaidThreshold; got != 4 {
		t.Fatalf("AutoAntiRaidThreshold = %d, want 4", got)
	}

	statusCtx := newModuleMessageContext(bot, chat, user, "/autoantiraid")
	if err := antiRaidModule.autoAntiRaid(bot, statusCtx); err != ext.EndGroups {
		t.Fatalf("autoAntiRaid(status) error = %v, want EndGroups", err)
	}

	offCtx := newModuleMessageContext(bot, chat, user, "/autoantiraid off")
	if err := antiRaidModule.autoAntiRaid(bot, offCtx); err != ext.EndGroups {
		t.Fatalf("autoAntiRaid(off) error = %v, want EndGroups", err)
	}
	if got := antiraid.GetAntiRaidSettings(chat.Id).AutoAntiRaidThreshold; got != 0 {
		t.Fatalf("AutoAntiRaidThreshold = %d, want 0 after off", got)
	}
}

func TestAutoAntiRaidCommandHandlesInvalidAndNoopBranches(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, text := range []string{
		"/autoantiraid off",
		"/autoantiraid nope",
		"/autoantiraid -1",
	} {
		ctx := newModuleMessageContext(bot, chat, user, text)
		if err := antiRaidModule.autoAntiRaid(bot, ctx); err != ext.EndGroups {
			t.Fatalf("autoAntiRaid(%q) error = %v, want EndGroups", text, err)
		}
	}

	if err := antiraid.SetAutoAntiRaidThreshold(chat.Id, 3); err != nil {
		t.Fatalf("SetAutoAntiRaidThreshold setup error = %v", err)
	}
	noChangeCtx := newModuleMessageContext(bot, chat, user, "/autoantiraid 3")
	if err := antiRaidModule.autoAntiRaid(bot, noChangeCtx); err != ext.EndGroups {
		t.Fatalf("autoAntiRaid(no change) error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 4 {
		t.Fatalf("sendMessage calls = %d, want validation and no-change replies", len(calls))
	}
}

func TestAntiRaidCallbackTogglesState(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chat.Id)
	})

	onCtx := newModuleCallbackContext(
		bot,
		chat,
		user,
		encodeCallbackData("antiraid", map[string]string{"a": "on"}),
	)
	if err := antiRaidModule.callbackHandler(bot, onCtx); err != ext.EndGroups {
		t.Fatalf("callbackHandler(on) error = %v, want EndGroups", err)
	}
	if !antiRaidModule.isRaidActive(chat.Id) {
		t.Fatal("raid was not activated by callback")
	}

	offCtx := newModuleCallbackContext(
		bot,
		chat,
		user,
		encodeCallbackData("antiraid", map[string]string{"a": "off"}),
	)
	if err := antiRaidModule.callbackHandler(bot, offCtx); err != ext.EndGroups {
		t.Fatalf("callbackHandler(off) error = %v, want EndGroups", err)
	}
	if antiRaidModule.isRaidActive(chat.Id) {
		t.Fatal("raid stayed active after callback off")
	}
	if calls := client.callsFor("answerCallbackQuery"); len(calls) != 2 {
		t.Fatalf("answerCallbackQuery calls = %d, want 2", len(calls))
	}
}

func TestAntiRaidCallbackRejectsInvalidAndUnknownActions(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	tests := []struct {
		name string
		data string
		want error
	}{
		{
			name: "malformed",
			data: "antiraid.bad.extra",
			want: ext.ContinueGroups,
		},
		{
			name: "unknown action",
			data: encodeCallbackData("antiraid", map[string]string{"a": "later"}),
			want: ext.EndGroups,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleCallbackContext(bot, chat, user, tt.data)
			if err := antiRaidModule.callbackHandler(bot, ctx); err != tt.want {
				t.Fatalf("callbackHandler(%q) error = %v, want %v", tt.data, err, tt.want)
			}
		})
	}
	if calls := client.callsFor("answerCallbackQuery"); len(calls) != 1 {
		t.Fatalf("answerCallbackQuery calls = %d, want unknown action acknowledged", len(calls))
	}
}

func TestAntiRaidOnJoinBansDuringActiveRaid(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	user := gotgbot.User{Id: 4249, FirstName: "Raider"}
	if enabled, err := antiRaidModule.enableRaid(chat.Id, 3600); err != nil || !enabled {
		t.Fatalf("enableRaid() = (%v, %v), want true, nil", enabled, err)
	}
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chat.Id)
	})

	msg := &gotgbot.Message{
		MessageId:      202,
		Date:           1,
		Chat:           chat,
		From:           &user,
		NewChatMembers: []gotgbot.User{user},
	}
	ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 202, Message: msg}, nil)
	if err := antiRaidModule.onJoin(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("onJoin() error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("banChatMember"); len(calls) != 1 {
		t.Fatalf("banChatMember calls = %d, want 1", len(calls))
	}
}

// TestAntiRaidOnJoinExemptsApprovedAndAdminDuringActiveRaid keeps the watcher
// seam honest: even with the raid window open, approved users and chat admins
// are never banned.
func TestAntiRaidOnJoinExemptsApprovedAndAdminDuringActiveRaid(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	approved := gotgbot.User{Id: 4251, FirstName: "Approved"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	raider := gotgbot.User{Id: 4252, FirstName: "Raider"}

	if err := approvals.AddApprovedUser(chat.Id, approved.Id, admin.Id, "trusted"); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}
	if enabled, err := antiRaidModule.enableRaid(chat.Id, 3600); err != nil || !enabled {
		t.Fatalf("enableRaid() = (%v, %v), want (true, nil)", enabled, err)
	}
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chat.Id)
	})

	msg := &gotgbot.Message{
		MessageId:      203,
		Date:           1,
		Chat:           chat,
		From:           &raider,
		NewChatMembers: []gotgbot.User{approved, admin, raider},
	}
	ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 203, Message: msg}, nil)
	if err := antiRaidModule.onJoin(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("onJoin() error = %v, want ContinueGroups", err)
	}

	calls := client.callsFor("banChatMember")
	if len(calls) != 1 {
		t.Fatalf("banChatMember calls = %d, want only the unexempt raider", len(calls))
	}
	if got := fmt.Sprint(calls[0].Params["user_id"]); got != fmt.Sprint(raider.Id) {
		t.Fatalf("banned user_id = %s, want %d", got, raider.Id)
	}
}

func TestBanRaidMemberRejectsUnsafeActionTimes(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}

	banRaidMember(bot, &chat, 4249, 29)
	banRaidMember(bot, &chat, 4250, 366*24*60*60+1)
	if calls := client.callsFor("banChatMember"); len(calls) != 0 {
		t.Fatalf("banChatMember calls = %d, want unsafe action times rejected", len(calls))
	}

	banRaidMember(bot, &chat, 4251, 30)
	if calls := client.callsFor("banChatMember"); len(calls) != 1 {
		t.Fatalf("banChatMember calls = %d, want exact 30-second action accepted", len(calls))
	}
}

func TestAntiRaidJoinRunsBeforeGreetingsGroup(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	member := gotgbot.User{Id: 4252, FirstName: "Raider"}
	if enabled, err := antiRaidModule.enableRaid(chat.Id, 3600); err != nil || !enabled {
		t.Fatalf("enableRaid() = (%v, %v), want true, nil", enabled, err)
	}
	t.Cleanup(func() {
		StopAntiRaidExpiryPoller()
		_, _ = antiRaidModule.disableRaid(chat.Id)
	})

	greetingsHandled := false
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return msg.NewChatMembers != nil },
		func(_ *gotgbot.Bot, _ *ext.Context) error {
			greetingsHandled = true
			return ext.EndGroups
		},
	))
	LoadAntiRaid(dispatcher)

	update := &gotgbot.Update{
		UpdateId: 303,
		Message: &gotgbot.Message{
			MessageId:      303,
			Date:           1,
			Chat:           chat,
			From:           &member,
			NewChatMembers: []gotgbot.User{member},
		},
	}
	if err := dispatcher.ProcessUpdate(bot, update, nil); err != nil {
		t.Fatalf("ProcessUpdate() error = %v", err)
	}
	if calls := client.callsFor("banChatMember"); len(calls) != 1 {
		t.Fatalf("banChatMember calls = %d, want AntiRaid to run before group 0 consumes the join", len(calls))
	}
	if !greetingsHandled {
		t.Fatal("group 0 greetings handler did not run after AntiRaid")
	}
}

func TestAntiRaidOnJoinSkipsIneligibleUpdates(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	member := gotgbot.User{Id: 4250, FirstName: "Member"}

	noChatCtx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 301}, nil)
	if err := antiRaidModule.onJoin(bot, noChatCtx); err != ext.ContinueGroups {
		t.Fatalf("onJoin(no chat) error = %v, want ContinueGroups", err)
	}

	privateChat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "private", Title: "Private"}
	privateCtx := newModuleMessageContext(bot, privateChat, member, "joined")
	privateCtx.EffectiveMessage.NewChatMembers = []gotgbot.User{member}
	if err := antiRaidModule.onJoin(bot, privateCtx); err != ext.ContinueGroups {
		t.Fatalf("onJoin(private) error = %v, want ContinueGroups", err)
	}

	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
	if err := approvals.AddApprovedUser(chat.Id, member.Id, 777000, "trusted"); err != nil {
		t.Fatalf("AddApprovedUser setup error = %v", err)
	}
	msg := &gotgbot.Message{
		MessageId: 302,
		Date:      1,
		Chat:      chat,
		From:      &member,
		NewChatMembers: []gotgbot.User{
			{Id: bot.Id, FirstName: "Alita"},
			member,
			{Id: 777000, FirstName: "Telegram"},
		},
	}
	ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 302, Message: msg}, nil)
	if err := antiRaidModule.onJoin(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("onJoin(skip members) error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("banChatMember"); len(calls) != 0 {
		t.Fatalf("banChatMember calls = %d, want no bans for skipped members", len(calls))
	}
}

// TestAntiRaidTrackJoinTriggersAtThreshold characterizes the join-counting logic in
// trackJoin: the count must stay below T for the first T-1 joins and reach T on
// the T-th join, with no overlap between distinct chat IDs.
func TestAntiRaidTrackJoinTriggersAtThreshold(t *testing.T) {

	chatID := uniqueModuleChatID()
	threshold := 3

	// Clean up join tracking state for the chat after the test.
	t.Cleanup(func() {
		clearJoinTracking(chatID)
	})

	// First T-1 joins must not meet the threshold.
	for i := 0; i < threshold-1; i++ {
		userID := int64(1000 + i)
		count, err := trackJoin(chatID, userID)
		if err != nil {
			t.Fatalf("trackJoin(%d, %d) error = %v", chatID, userID, err)
		}
		if count >= threshold {
			t.Fatalf("join %d: count = %d, want < %d (threshold not yet reached)", i+1, count, threshold)
		}
	}

	// T-th join must meet or exceed the threshold.
	count, err := trackJoin(chatID, int64(1000+threshold-1))
	if err != nil {
		t.Fatalf("trackJoin (T-th) error = %v", err)
	}
	if count < threshold {
		t.Fatalf("T-th join: count = %d, want >= %d (threshold should be reached)", count, threshold)
	}
	entries, ok := state.Get[[]joinEntry](context.Background(), joinsKey(chatID))
	if !ok || len(entries) < threshold {
		t.Fatalf("join tracking entries = %v (ok=%v), want >= %d entries in state", entries, ok, threshold)
	}

	// A separate chat ID must have an independent counter.
	otherChatID := uniqueModuleChatID()
	t.Cleanup(func() {
		clearJoinTracking(otherChatID)
	})
	otherCount, err := trackJoin(otherChatID, 9999)
	if err != nil {
		t.Fatalf("trackJoin(other chat) error = %v", err)
	}
	if otherCount != 1 {
		t.Fatalf("other chat join count = %d, want 1 (counters are independent)", otherCount)
	}
}

// TestAntiRaidOnJoinAppliesConfiguredAction characterizes two key onJoin branches:
//  1. No action when raid is inactive and AutoAntiRaidThreshold is 0.
//  2. Ban + raid activation when the threshold is reached (via miniredis).
func TestAntiRaidOnJoinAppliesConfiguredAction(t *testing.T) {
	t.Run("no action when raid inactive and threshold disabled", func(t *testing.T) {
		client := newModuleBotClient()
		bot := newModuleTestBot(client)
		chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
		raider := gotgbot.User{Id: 5001, FirstName: "Raider"}

		// Ensure threshold is 0 (default) so no auto-raid logic runs.
		if err := antiraid.SetAutoAntiRaidThreshold(chat.Id, 0); err != nil {
			t.Fatalf("SetAutoAntiRaidThreshold setup error = %v", err)
		}

		msg := &gotgbot.Message{
			MessageId:      401,
			Date:           1,
			Chat:           chat,
			From:           &raider,
			NewChatMembers: []gotgbot.User{raider},
		}
		ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 401, Message: msg}, nil)
		if err := antiRaidModule.onJoin(bot, ctx); err != ext.ContinueGroups {
			t.Fatalf("onJoin(no threshold) error = %v, want ContinueGroups", err)
		}
		if calls := client.callsFor("banChatMember"); len(calls) != 0 {
			t.Fatalf("banChatMember calls = %d, want no action when threshold disabled", len(calls))
		}
	})

	t.Run("auto-triggers raid and bans joiner at threshold", func(t *testing.T) {

		client := newModuleBotClient()
		bot := newModuleTestBot(client)
		chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
		raider := gotgbot.User{Id: 5002, FirstName: "Raider"}

		// Set threshold to 1: the very first tracked join must trigger auto-raid.
		if err := antiraid.SetAutoAntiRaidThreshold(chat.Id, 1); err != nil {
			t.Fatalf("SetAutoAntiRaidThreshold(1) error = %v", err)
		}
		t.Cleanup(func() {
			_, _ = antiRaidModule.disableRaid(chat.Id)
			clearJoinTracking(chat.Id)
		})

		msg := &gotgbot.Message{
			MessageId:      402,
			Date:           1,
			Chat:           chat,
			From:           &raider,
			NewChatMembers: []gotgbot.User{raider},
		}
		ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 402, Message: msg}, nil)
		if err := antiRaidModule.onJoin(bot, ctx); err != ext.ContinueGroups {
			t.Fatalf("onJoin(auto-trigger) error = %v, want ContinueGroups", err)
		}

		// The raid must now be active.
		if !antiRaidModule.isRaidActive(chat.Id) {
			t.Fatal("isRaidActive = false after threshold reached, want true")
		}
		// The triggering joiner must have been banned.
		if calls := client.callsFor("banChatMember"); len(calls) != 1 {
			t.Fatalf("banChatMember calls = %d, want 1 (triggering joiner)", len(calls))
		}
	})

	t.Run("bans the rest of a batch after auto-triggering once", func(t *testing.T) {

		client := newModuleBotClient()
		bot := newModuleTestBot(client)
		chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Raid Chat"}
		members := []gotgbot.User{
			{Id: 5101, FirstName: "First"},
			{Id: 5102, FirstName: "Second"},
			{Id: 5103, FirstName: "Third"},
		}

		if err := antiraid.SetAutoAntiRaidThreshold(chat.Id, 2); err != nil {
			t.Fatalf("SetAutoAntiRaidThreshold(2) error = %v", err)
		}
		t.Cleanup(func() {
			_, _ = antiRaidModule.disableRaid(chat.Id)
			clearJoinTracking(chat.Id)
		})

		msg := &gotgbot.Message{
			MessageId:      403,
			Date:           1,
			Chat:           chat,
			From:           &members[0],
			NewChatMembers: members,
		}
		ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 403, Message: msg}, nil)
		if err := antiRaidModule.onJoin(bot, ctx); err != ext.ContinueGroups {
			t.Fatalf("onJoin(batch) error = %v, want ContinueGroups", err)
		}
		if calls := client.callsFor("banChatMember"); len(calls) != 2 {
			t.Fatalf("banChatMember calls = %d, want triggering and subsequent members", len(calls))
		}
		if calls := client.callsFor("sendMessage"); len(calls) != 1 {
			t.Fatalf("sendMessage calls = %d, want one auto-trigger notification", len(calls))
		}
	})
}

// TestAntiRaidCheckExpiredRaidsReleasesAfterWindow verifies that the poller
// persists expiry while leaving a live raid untouched.
func TestAntiRaidCheckExpiredRaidsReleasesAfterWindow(t *testing.T) {
	// --- Scenario A: expired raid ---
	expiredChatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(expiredChatID)
		clearJoinTracking(expiredChatID)
	})

	seedRaidWindow(t, expiredChatID, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

	// --- Scenario B: still-active raid ---
	activeChatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(activeChatID)
		clearJoinTracking(activeChatID)
	})

	seedRaidWindow(t, activeChatID, time.Now(), time.Now().Add(time.Hour))

	// Run the expiry check — must not return an error or panic.
	antiRaidModule.checkExpiredRaids(context.Background())

	// The expired state is persisted as inactive.
	expiredSt := getRaidState(expiredChatID)
	if expiredSt.Active {
		t.Fatalf("getRaidState(expired).Active = true after checkExpiredRaids, want false")
	}
	if antiRaidModule.isRaidActive(expiredChatID) {
		t.Fatal("isRaidActive(expired) = true after checkExpiredRaids, want false")
	}

	// A non-expired raid remains active.
	activeSt := getRaidState(activeChatID)
	if !activeSt.Active {
		t.Fatalf("getRaidState(active).Active = false after checkExpiredRaids, want true (non-expired raid must not be released)")
	}
	if !antiRaidModule.isRaidActive(activeChatID) {
		t.Fatal("isRaidActive(active) = false after checkExpiredRaids, want true (non-expired raid must stay active)")
	}
}

// TestAntiRaidLongRaidWindowSurvivesCacheLoss covers week-long raids: the
// window is stored in SQLite, so losing the cache no longer shortens it.
func TestAntiRaidLongRaidWindowSurvivesCacheLoss(t *testing.T) {
	chatID := uniqueModuleChatID()
	t.Cleanup(func() {
		_, _ = antiRaidModule.disableRaid(chatID)
	})

	weekSeconds := int((7 * 24 * time.Hour).Seconds())
	if enabled, err := antiRaidModule.enableRaid(chatID, weekSeconds); err != nil || !enabled {
		t.Fatalf("enableRaid(week) = (%v, %v), want (true, nil)", enabled, err)
	}

	state := getRaidState(chatID)
	if !state.Active {
		t.Fatal("week-long raid read as inactive without a cache")
	}
	if remaining := state.ExpiresAt - time.Now().Unix(); remaining <= int64((6 * 24 * time.Hour).Seconds()) {
		t.Fatalf("remaining raid window = %ds, want longer than six days", remaining)
	}
}
