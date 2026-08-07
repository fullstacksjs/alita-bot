package chat_status

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/divkix/Alita_Robot/alita/utils/cache"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

// mutableAdminClient serves an administrator list that a test can change, so
// promotion, demotion, and membership updates can be simulated.
type mutableAdminClient struct {
	mu     sync.Mutex
	admins []int64
	calls  map[string]int
}

func newMutableAdminClient(admins ...int64) *mutableAdminClient {
	return &mutableAdminClient{admins: admins, calls: make(map[string]int)}
}

func (c *mutableAdminClient) setAdmins(admins ...int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.admins = admins
}

func (c *mutableAdminClient) callCount(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[method]
}

func (c *mutableAdminClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	c.mu.Lock()
	c.calls[method]++
	admins := make([]int64, len(c.admins))
	copy(admins, c.admins)
	c.mu.Unlock()

	switch method {
	case "getChat":
		return json.RawMessage(`{"id":-1002,"type":"supergroup","title":"Admin Cache Chat"}`), nil
	case "getChatMember":
		userID := fmt.Sprint(params["user_id"])
		for _, admin := range admins {
			if fmt.Sprint(admin) == userID {
				return json.RawMessage(fmt.Sprintf(
					`{"status":"administrator","user":{"id":%s,"is_bot":false,"first_name":"Admin"}}`, userID,
				)), nil
			}
		}
		return json.RawMessage(fmt.Sprintf(
			`{"status":"member","user":{"id":%s,"is_bot":false,"first_name":"Member"}}`, userID,
		)), nil
	case "getChatAdministrators":
		members := make([]string, 0, len(admins))
		for _, admin := range admins {
			members = append(members, fmt.Sprintf(
				`{"status":"administrator","user":{"id":%d,"is_bot":false,"first_name":"Admin"}}`, admin,
			))
		}
		return json.RawMessage("[" + strings.Join(members, ",") + "]"), nil
	default:
		return json.RawMessage(`true`), nil
	}
}

func (c *mutableAdminClient) GetAPIURL(*gotgbot.RequestOpts) string {
	return "https://api.telegram.org"
}

func (c *mutableAdminClient) FileURL(token string, path string, _ *gotgbot.RequestOpts) string {
	return "https://api.telegram.org/file/bot" + token + "/" + path
}

func newMutableAdminBot(client *mutableAdminClient) *gotgbot.Bot {
	return &gotgbot.Bot{
		Token:     "999:test",
		BotClient: client,
		User:      gotgbot.User{Id: 999, IsBot: true, FirstName: "Bot"},
	}
}

func TestIssue25_PermissionsStayCorrectAcrossPromotionDemotionAndRestart(t *testing.T) {
	coldAdminCache(t)

	const chatID = int64(-1002)
	client := newMutableAdminClient(999, 10)
	bot := newMutableAdminBot(client)

	if !IsUserAdmin(bot, chatID, 10) {
		t.Fatal("IsUserAdmin(10) = false, want true for the seeded admin")
	}
	if IsUserAdmin(bot, chatID, 20) {
		t.Fatal("IsUserAdmin(20) = true, want false before promotion")
	}

	// Promotion: without invalidation the cached list is authoritative, and the
	// admin-status handler invalidates and reloads on every membership update.
	client.setAdmins(999, 10, 20)
	cache.InvalidateAdminCache(chatID)
	cache.LoadAdminCache(bot, chatID)
	if !IsUserAdmin(bot, chatID, 20) {
		t.Fatal("IsUserAdmin(20) = false after promotion, want true")
	}

	// Demotion.
	client.setAdmins(999, 20)
	cache.InvalidateAdminCache(chatID)
	cache.LoadAdminCache(bot, chatID)
	if IsUserAdmin(bot, chatID, 10) {
		t.Fatal("IsUserAdmin(10) = true after demotion, want false")
	}

	// Membership change: the demoted user leaves the chat entirely.
	client.setAdmins(999)
	cache.InvalidateAdminCache(chatID)
	cache.LoadAdminCache(bot, chatID)
	if IsUserAdmin(bot, chatID, 20) {
		t.Fatal("IsUserAdmin(20) = true after leaving, want false")
	}

	// Simulated restart drops the in-process cache; the next check reloads from
	// Telegram and still answers correctly.
	client.setAdmins(999, 10)
	state.SimulateRestart()
	if found, _ := cache.GetAdminCacheList(chatID); found {
		t.Fatal("GetAdminCacheList() found an entry after SimulateRestart, want empty cache")
	}
	if !IsUserAdmin(bot, chatID, 10) {
		t.Fatal("IsUserAdmin(10) = false after restart, want reload from Telegram")
	}
}

func TestIssue25_ConcurrentAdminLoadsCollapseIntoOneTelegramLookup(t *testing.T) {
	coldAdminCache(t)

	const chatID = int64(-1002)
	client := newMutableAdminClient(999, 10)
	bot := newMutableAdminBot(client)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if loaded := cache.LoadAdminCache(bot, chatID); len(loaded.UserInfo) != 2 {
				t.Errorf("LoadAdminCache() = %+v, want two administrators", loaded)
			}
		}()
	}
	wg.Wait()

	if got := client.callCount("getChatAdministrators"); got > 2 {
		t.Fatalf("getChatAdministrators calls = %d, want concurrent loads collapsed", got)
	}
}

func TestIssue25_AdminCacheMissFallsBackToTelegram(t *testing.T) {
	coldAdminCache(t)

	const chatID = int64(-1002)
	client := newMutableAdminClient(999, 10)
	bot := newMutableAdminBot(client)

	// Cold cache: the lookup must be answered by Telegram rather than failing.
	if found, _ := cache.GetAdminCacheUser(chatID, 10); found {
		t.Fatal("GetAdminCacheUser() found = true on a cold cache")
	}
	member, ok := getUserMemberWithCache(bot, &gotgbot.Chat{Id: chatID, Type: "supergroup"}, 10, "issue25")
	if !ok || member.User.Id != 10 {
		t.Fatalf("getUserMemberWithCache() = (%+v, %v), want the Telegram member", member, ok)
	}
}

func TestIssue25_AdminCacheEntriesAreNotAliasedAcrossReads(t *testing.T) {
	coldAdminCache(t)

	const chatID = int64(-1002)
	client := newMutableAdminClient(999, 10)
	bot := newMutableAdminBot(client)

	cache.LoadAdminCache(bot, chatID)

	found, first := cache.GetAdminCacheList(chatID)
	if !found {
		t.Fatal("GetAdminCacheList() found = false after LoadAdminCache")
	}
	delete(first.UserMap, 10)
	first.UserInfo = first.UserInfo[:0]

	if !IsUserAdmin(bot, chatID, 10) {
		t.Fatal("IsUserAdmin(10) = false after a caller mutated its own copy")
	}
}
