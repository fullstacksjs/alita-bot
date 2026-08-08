package modules

import (
	"errors"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func TestBotJoinedGroupIgnoresPrivateChats(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: 42, Type: "private", FirstName: "Private"}
	user := gotgbot.User{Id: 42, FirstName: "Private"}
	ctx := newModuleMessageContext(bot, chat, user, "bot joined")

	if err := botJoinedGroup(bot, ctx); err != ext.EndGroups {
		t.Fatalf("botJoinedGroup() error = %v, want EndGroups for private chat", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 0 {
		t.Fatalf("sendMessage calls = %d, want none for private chat", len(calls))
	}
	if calls := client.callsFor("leaveChat"); len(calls) != 0 {
		t.Fatalf("leaveChat calls = %d, want none for private chat", len(calls))
	}
}

func TestBotJoinedGroupLeavesBasicGroupAfterMigrationNotice(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "group", Title: "Basic Group"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	ctx := newModuleMessageContext(bot, chat, user, "bot joined")

	if err := botJoinedGroup(bot, ctx); err != ext.EndGroups {
		t.Fatalf("botJoinedGroup() error = %v, want EndGroups for basic group", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want migration notice", len(calls))
	}
	if calls := client.callsFor("leaveChat"); len(calls) != 1 {
		t.Fatalf("leaveChat calls = %d, want bot to leave basic group", len(calls))
	}
}

func TestBotJoinedGroupLeavesChannelWithoutMigrationNotice(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "channel", Title: "Broadcast"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	ctx := newModuleMessageContext(bot, chat, user, "bot joined")

	if err := botJoinedGroup(bot, ctx); err != ext.EndGroups {
		t.Fatalf("botJoinedGroup() error = %v, want EndGroups for channel", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 0 {
		t.Fatalf("sendMessage calls = %d, want no migration notice for channel", len(calls))
	}
	if calls := client.callsFor("leaveChat"); len(calls) != 1 {
		t.Fatalf("leaveChat calls = %d, want bot to leave channel", len(calls))
	}
}

func TestBotJoinedSupergroupSendsWelcome(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Super Group"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	ctx := newModuleMessageContext(bot, chat, user, "bot joined")

	if err := botJoinedGroup(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("botJoinedGroup() error = %v, want ContinueGroups for supergroup", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want welcome message", len(calls))
	}
	if calls := client.callsFor("leaveChat"); len(calls) != 0 {
		t.Fatalf("leaveChat calls = %d, want none for supergroup", len(calls))
	}
}

func TestBotJoinedGroupPropagatesGotgbotRequestErrors(t *testing.T) {
	requestErr := errors.New("telegram request failed")
	user := gotgbot.User{Id: 42, FirstName: "Member"}

	for _, tt := range []struct {
		name   string
		chat   gotgbot.Chat
		method string
	}{
		{
			name:   "basic group migration notice",
			chat:   gotgbot.Chat{Id: uniqueModuleChatID(), Type: "group", Title: "Basic Group"},
			method: "sendMessage",
		},
		{
			name:   "basic group leave",
			chat:   gotgbot.Chat{Id: uniqueModuleChatID(), Type: "group", Title: "Basic Group"},
			method: "leaveChat",
		},
		{
			name:   "channel leave",
			chat:   gotgbot.Chat{Id: uniqueModuleChatID(), Type: "channel", Title: "Broadcast"},
			method: "leaveChat",
		},
		{
			name:   "supergroup welcome",
			chat:   gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Super Group"},
			method: "sendMessage",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			client.errors[tt.method] = requestErr
			ctx := newModuleMessageContext(bot, tt.chat, user, "bot joined")

			err := botJoinedGroup(bot, ctx)
			if !errors.Is(err, requestErr) {
				t.Fatalf("botJoinedGroup() error = %v, want request error", err)
			}
		})
	}
}

func TestAdminCacheAutoUpdateSkipsMissingEffectiveChat(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	ctx := ext.NewContext(bot, &gotgbot.Update{UpdateId: 99}, nil)

	if err := adminCacheAutoUpdate(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("adminCacheAutoUpdate() error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("getChatAdministrators"); len(calls) != 0 {
		t.Fatalf("getChatAdministrators calls = %d, want none for missing chat", len(calls))
	}
}

func TestAdminCacheAutoUpdateReloadsAdminList(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Admin Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	ctx := newModuleMessageContext(bot, chat, admin, "admin changed")

	if err := adminCacheAutoUpdate(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("adminCacheAutoUpdate() error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("getChatAdministrators"); len(calls) != 1 {
		t.Fatalf("getChatAdministrators calls = %d, want cache reload", len(calls))
	}
}

func TestBotUpdatesLoadersRegisterExpectedHandlers(t *testing.T) {
	moduleDispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})
	LoadBotUpdates(moduleDispatcher)
	if removed := moduleDispatcher.RemoveGroup(-1); !removed {
		t.Fatal("LoadBotUpdates did not register join handler in group -1")
	}
	if removed := moduleDispatcher.RemoveGroup(0); !removed {
		t.Fatal("LoadBotUpdates did not register standard handlers in group 0")
	}
}
