package modules

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func TestStartAndHelpWorkInPrivateAndGroupChats(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4301, FirstName: "Helper"}
	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Helper"}
	groupChat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Help Chat"}

	for _, tc := range []struct {
		chat gotgbot.Chat
		text string
		run  func(*gotgbot.Bot, *ext.Context) error
	}{
		{privateChat, "/start", DefaultHelpRegistry().start},
		{groupChat, "/start", DefaultHelpRegistry().start},
		{privateChat, "/help", DefaultHelpRegistry().help},
		{groupChat, "/help", DefaultHelpRegistry().help},
		{privateChat, "/help bans", DefaultHelpRegistry().help},
	} {
		ctx := newModuleMessageContext(bot, tc.chat, user, tc.text)
		if err := tc.run(bot, ctx); err != ext.EndGroups {
			t.Fatalf("%s in %s returned %v, want EndGroups", tc.text, tc.chat.Type, err)
		}
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 5 {
		t.Fatalf("sendMessage calls = %d, want 5", len(calls))
	}
}

func TestHelpButtonHandlesMenusAndRetainedModule(t *testing.T) {
	registry := DefaultHelpRegistry()
	registry.AbleMap["Bans"] = true
	initHelpButtons()

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4302, FirstName: "Helper"}
	chat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Helper"}

	for _, module := range []string{"Help", "BackStart", "Bans"} {
		ctx := newModuleCallbackContext(bot, chat, user, encodeCallbackData("helpq", map[string]string{"m": module}))
		if err := DefaultHelpRegistry().helpButtonHandler(bot, ctx); err != ext.EndGroups {
			t.Fatalf("help callback %s returned %v, want EndGroups", module, err)
		}
	}
}

func TestLoadHelpRegistersRetainedHandlers(t *testing.T) {
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})
	LoadHelp(dispatcher)
}
