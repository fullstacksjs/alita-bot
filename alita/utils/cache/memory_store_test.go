package cache

import (
	"context"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/divkix/Alita_Robot/alita/utils/constants"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

func TestAdminCacheRoundTripWithMemoryStore(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-100123)
	adminViaMap := gotgbot.MergedChatMember{
		Status: "administrator",
		User:   gotgbot.User{Id: 10, FirstName: "Map Admin"},
	}
	adminViaList := gotgbot.MergedChatMember{
		Status: "administrator",
		User:   gotgbot.User{Id: 11, FirstName: "List Admin"},
	}
	adminCache := AdminCache{
		ChatId:   chatID,
		UserInfo: []gotgbot.MergedChatMember{adminViaList},
		UserMap:  map[int64]gotgbot.MergedChatMember{10: adminViaMap},
		Cached:   true,
	}

	state.Set(context.Background(), adminCacheKey(chatID), adminCache, time.Minute)

	found, gotCache := GetAdminCacheList(chatID)
	if !found || !gotCache.Cached || gotCache.ChatId != chatID {
		t.Fatalf("GetAdminCacheList() = (%v, %+v), want cached chat %d", found, gotCache, chatID)
	}

	found, gotMember := GetAdminCacheUser(chatID, 10)
	if !found || gotMember.User.Id != 10 {
		t.Fatalf("GetAdminCacheUser(map user) = (%v, %+v), want user 10", found, gotMember)
	}

	found, gotMember = GetAdminCacheUser(chatID, 11)
	if !found || gotMember.User.Id != 11 {
		t.Fatalf("GetAdminCacheUser(list fallback) = (%v, %+v), want user 11", found, gotMember)
	}

	found, gotMember = GetAdminCacheUser(chatID, 42)
	if found || gotMember.User.Id != 0 {
		t.Fatalf("GetAdminCacheUser(missing) = (%v, %+v), want miss", found, gotMember)
	}

	InvalidateAdminCache(chatID)
	if found, _ := GetAdminCacheList(chatID); found {
		t.Fatal("GetAdminCacheList() found cache after InvalidateAdminCache")
	}
}

func TestRestrictedCacheUsesMemoryStoreWithoutRedis(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-100456)
	MarkChatRestricted(chatID)
	if !IsChatRestricted(chatID) {
		t.Fatal("IsChatRestricted() = false after MarkChatRestricted")
	}

	MarkChatNotRestricted(chatID)
	if IsChatRestricted(chatID) {
		t.Fatal("IsChatRestricted() = true after MarkChatNotRestricted")
	}
}

func TestIsChatRestrictedAllowsMalformedAndStaleEntriesWithMemoryStore(t *testing.T) {
	state.SimulateRestart()

	const malformedChatID = int64(-100457)
	state.Set(context.Background(), restrictedChatKey(malformedChatID), "not-a-timestamp", constants.RestrictedCacheTTL)
	if IsChatRestricted(malformedChatID) {
		t.Fatal("IsChatRestricted(malformed timestamp) = true, want false")
	}

	const staleChatID = int64(-100458)
	stale := time.Now().Add(-constants.RestrictedProbeInterval - time.Second).Format(time.RFC3339)
	state.Set(context.Background(), restrictedChatKey(staleChatID), stale, constants.RestrictedCacheTTL)
	if IsChatRestricted(staleChatID) {
		t.Fatal("IsChatRestricted(stale timestamp without probe lock) = true, want false")
	}
}
