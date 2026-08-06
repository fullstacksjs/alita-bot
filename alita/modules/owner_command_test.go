package modules

import (
	"errors"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db"
)

func withOwnerID(t *testing.T, ownerID int64) {
	t.Helper()

	previousOwnerID := config.AppConfig.OwnerId
	config.AppConfig.OwnerId = ownerID
	t.Cleanup(func() {
		config.AppConfig.OwnerId = previousOwnerID
	})
}

func TestOwnerLeaveChatHandlesMissingArgsAndRequestErrors(t *testing.T) {
	withOwnerID(t, 777000)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "private", FirstName: "Owner"}
	owner := gotgbot.User{Id: 777000, FirstName: "Owner"}

	missingClient := newModuleBotClient()
	missingBot := newModuleTestBot(missingClient)
	missingCtx := newModuleMessageContext(missingBot, chat, owner, "/leavechat")
	if err := ownerModule.leaveChat(missingBot, missingCtx); err != ext.ContinueGroups {
		t.Fatalf("leaveChat(missing args) error = %v, want ContinueGroups", err)
	}
	if calls := missingClient.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want missing-arg reply", len(calls))
	}

	requestClient := newModuleBotClient()
	requestClient.errors["leaveChat"] = errors.New("leave failed")
	requestBot := newModuleTestBot(requestClient)
	requestCtx := newModuleMessageContext(requestBot, chat, owner, "/leavechat -100123")
	if err := ownerModule.leaveChat(requestBot, requestCtx); err == nil {
		t.Fatal("leaveChat(request error) = nil, want error")
	}
}

func TestOwnerStatsPropagatesSendAndEditErrors(t *testing.T) {
	withOwnerID(t, 777000)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "private", FirstName: "Owner"}
	owner := gotgbot.User{Id: 777000, FirstName: "Owner"}

	sendClient := newModuleBotClient()
	sendClient.errors["sendMessage"] = errors.New("send failed")
	sendBot := newModuleTestBot(sendClient)
	sendCtx := newModuleMessageContext(sendBot, chat, owner, "/stats")
	if err := ownerModule.getStats(sendBot, sendCtx); err == nil {
		t.Fatal("getStats(send error) = nil, want error")
	}

	editClient := newModuleBotClient()
	editClient.errors["editMessageText"] = errors.New("edit failed")
	editBot := newModuleTestBot(editClient)
	editCtx := newModuleMessageContext(editBot, chat, owner, "/stats")
	if err := ownerModule.getStats(editBot, editCtx); err == nil {
		t.Fatal("getStats(edit error) = nil, want error")
	}
}

// Bot-wide authorization is OWNER_ID only: no other identity may reach an
// owner operation, and no Telegram call is made on the rejected path.
func TestOwnerCommandsRejectNonOwnersWithoutTelegramCalls(t *testing.T) {
	withOwnerID(t, 777000)
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "private", FirstName: "Guest"}
	guest := gotgbot.User{Id: 42, FirstName: "Guest"}

	commands := map[string]func(*gotgbot.Bot, *ext.Context) error{
		"/chatinfo -100123":  ownerModule.chatInfo,
		"/chatlist":          ownerModule.chatList,
		"/leavechat -100123": ownerModule.leaveChat,
		"/stats":             ownerModule.getStats,
	}
	for text, handler := range commands {
		ctx := newModuleMessageContext(bot, chat, guest, text)
		if err := handler(bot, ctx); err != ext.ContinueGroups {
			t.Fatalf("%s error = %v, want ContinueGroups", text, err)
		}
	}
	if calls := client.calls; len(calls) != 0 {
		t.Fatalf("Telegram calls = %d, want none for unauthorized user", len(calls))
	}
}

func TestOwnerChatInfoLeaveChatAndStats(t *testing.T) {
	withOwnerID(t, 777000)

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "private", FirstName: "Owner"}
	owner := gotgbot.User{Id: 777000, FirstName: "Owner"}

	chatInfoCtx := newModuleMessageContext(bot, chat, owner, "/chatinfo -100123")
	if err := ownerModule.chatInfo(bot, chatInfoCtx); err != ext.ContinueGroups {
		t.Fatalf("chatInfo() error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("getChatMemberCount"); len(calls) != 1 {
		t.Fatalf("getChatMemberCount calls = %d, want 1", len(calls))
	}

	leaveCtx := newModuleMessageContext(bot, chat, owner, "/leavechat -100123")
	if err := ownerModule.leaveChat(bot, leaveCtx); err != ext.ContinueGroups {
		t.Fatalf("leaveChat() error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("leaveChat"); len(calls) != 1 {
		t.Fatalf("leaveChat calls = %d, want 1", len(calls))
	}

	statsCtx := newModuleMessageContext(bot, chat, owner, "/stats")
	if err := ownerModule.getStats(bot, statsCtx); err != ext.ContinueGroups {
		t.Fatalf("getStats() error = %v, want ContinueGroups", err)
	}
	editCalls := client.callsFor("editMessageText")
	if len(editCalls) != 1 {
		t.Fatalf("editMessageText calls = %d, want 1 stats edit", len(editCalls))
	}
	// Statistics are derived from tracked identities, not from a role table.
	if text := editCalls[0].Params["text"].(string); !strings.Contains(text, "users found in") {
		t.Fatalf("stats text %q missing tracked-identity counts", text)
	}
}

func TestOwnerChatListSendsDocument(t *testing.T) {
	withOwnerID(t, 777000)
	activeID := uniqueModuleChatID()
	if err := db.DB.Create(&db.Chat{ChatId: activeID, ChatName: "Active", IsInactive: false}).Error; err != nil {
		t.Fatalf("Create active chat failed: %v", err)
	}
	if err := db.DB.Create(&db.Chat{ChatId: uniqueModuleChatID(), ChatName: "Inactive", IsInactive: true}).Error; err != nil {
		t.Fatalf("Create inactive chat failed: %v", err)
	}

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "private", FirstName: "Owner"}
	owner := gotgbot.User{Id: 777000, FirstName: "Owner"}

	ctx := newModuleMessageContext(bot, chat, owner, "/chatlist")
	if err := ownerModule.chatList(bot, ctx); err != ext.EndGroups {
		t.Fatalf("chatList() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendDocument"); len(calls) != 1 {
		t.Fatalf("sendDocument calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("deleteMessage"); len(calls) != 1 {
		t.Fatalf("deleteMessage calls = %d, want status message deletion", len(calls))
	}
}
