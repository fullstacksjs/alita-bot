package modules

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/db"
	dbcache "github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/reactions"
)

func TestReactionCommandsManageDBRows(t *testing.T) {
	if db.DB == nil {
		t.Skip("requires database connection")
	}

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Reaction Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	t.Cleanup(func() {
		_ = reactions.ResetReactions(chat.Id)
	})

	addCtx := newModuleMessageContext(bot, chat, admin, "/addreaction hello 👍")
	if err := reactionsModule.addReaction(bot, addCtx); err != ext.EndGroups {
		t.Fatalf("addReaction() error = %v, want EndGroups", err)
	}
	if got := reactions.GetReactions(chat.Id)["hello"]; got != "👍" {
		t.Fatalf("stored reaction = %q, want 👍", got)
	}

	listCtx := newModuleMessageContext(bot, chat, admin, "/reactions")
	if err := reactionsModule.listReactions(bot, listCtx); err != ext.EndGroups {
		t.Fatalf("listReactions() error = %v, want EndGroups", err)
	}

	removeCtx := newModuleMessageContext(bot, chat, admin, "/removereaction hello")
	if err := reactionsModule.removeReaction(bot, removeCtx); err != ext.EndGroups {
		t.Fatalf("removeReaction() error = %v, want EndGroups", err)
	}
	if m := reactions.GetReactions(chat.Id); len(m) != 0 {
		t.Fatalf("reactions remained after removing final reaction: %v", m)
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 3 {
		t.Fatalf("sendMessage calls = %d, want 3", len(calls))
	}
}

func TestReactionCommandsHandleUsageAndMissingEntries(t *testing.T) {
	if db.DB == nil {
		t.Skip("requires database connection")
	}

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Reaction Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	t.Cleanup(func() {
		_ = reactions.ResetReactions(chat.Id)
	})

	for _, tt := range []struct {
		name string
		text string
		run  func(*gotgbot.Bot, *ext.Context) error
	}{
		{name: "add usage", text: "/addreaction keyword", run: reactionsModule.addReaction},
		{name: "remove usage", text: "/removereaction", run: reactionsModule.removeReaction},
		{name: "remove missing", text: "/removereaction hello", run: reactionsModule.removeReaction},
		{name: "list missing", text: "/reactions", run: reactionsModule.listReactions},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)
			if err := tt.run(bot, ctx); err != ext.EndGroups {
				t.Fatalf("%s error = %v, want EndGroups", tt.name, err)
			}
		})
	}

	if err := reactions.AddReaction(chat.Id, "hello", "👍"); err != nil {
		t.Fatalf("seed reaction: %v", err)
	}
	missingKeywordCtx := newModuleMessageContext(bot, chat, admin, "/removereaction absent")
	if err := reactionsModule.removeReaction(bot, missingKeywordCtx); err != ext.EndGroups {
		t.Fatalf("removeReaction(missing keyword) error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 5 {
		t.Fatalf("sendMessage calls = %d, want usage and missing-entry replies", len(calls))
	}
}

func TestCheckReactionsSetsMessageReactionForMatchingKeyword(t *testing.T) {
	if db.DB == nil {
		t.Skip("requires database connection")
	}

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Reaction Chat"}
	user := gotgbot.User{Id: 4307, FirstName: "Member"}
	t.Cleanup(func() {
		_ = reactions.ResetReactions(chat.Id)
	})

	if err := reactions.AddReaction(chat.Id, "hello", "👍"); err != nil {
		t.Fatalf("seed reaction: %v", err)
	}

	ctx := newModuleMessageContext(bot, chat, user, "well hello there")
	if err := reactionsModule.checkReactions(bot, ctx); err != ext.ContinueGroups {
		t.Fatalf("checkReactions() error = %v, want ContinueGroups", err)
	}
	if calls := client.callsFor("setMessageReaction"); len(calls) != 1 {
		t.Fatalf("setMessageReaction calls = %d, want 1", len(calls))
	}
}

func TestCheckReactionsNoopsForMissingMessageChatAndEmptyConfig(t *testing.T) {
	if db.DB == nil {
		t.Skip("requires database connection")
	}

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Reaction Chat"}
	user := gotgbot.User{Id: 4307, FirstName: "Member"}
	t.Cleanup(func() {
		_ = reactions.ResetReactions(chat.Id)
	})

	emptyTextCtx := newModuleMessageContext(bot, chat, user, "")
	if err := reactionsModule.checkReactions(bot, emptyTextCtx); err != ext.ContinueGroups {
		t.Fatalf("checkReactions(empty text) error = %v, want ContinueGroups", err)
	}

	noChatCtx := newModuleMessageContext(bot, chat, user, "hello")
	noChatCtx.EffectiveChat = nil
	if err := reactionsModule.checkReactions(bot, noChatCtx); err != ext.ContinueGroups {
		t.Fatalf("checkReactions(no chat) error = %v, want ContinueGroups", err)
	}

	emptyConfigCtx := newModuleMessageContext(bot, chat, user, "hello")
	if err := reactionsModule.checkReactions(bot, emptyConfigCtx); err != ext.ContinueGroups {
		t.Fatalf("checkReactions(empty config) error = %v, want ContinueGroups", err)
	}

	if calls := client.callsFor("setMessageReaction"); len(calls) != 0 {
		t.Fatalf("setMessageReaction calls = %d, want none for no-op branches", len(calls))
	}
}

// TestReactionCommandsWorkWithCacheDisabled verifies the DB-backed repository
// still functions (bypassing cache) when read-through cache is disabled.
func TestReactionCommandsWorkWithCacheDisabled(t *testing.T) {
	if db.DB == nil {
		t.Skip("requires database connection")
	}

	restore := dbcache.SetEnabled(false)
	t.Cleanup(restore)

	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Reaction Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	t.Cleanup(func() {
		_ = reactions.ResetReactions(chat.Id)
	})

	addCtx := newModuleMessageContext(bot, chat, admin, "/addreaction hello 👍")
	if err := reactionsModule.addReaction(bot, addCtx); err != ext.EndGroups {
		t.Fatalf("addReaction(nil marshal) error = %v, want EndGroups", err)
	}
	if got := reactions.GetReactions(chat.Id)["hello"]; got != "👍" {
		t.Fatalf("stored reaction with nil marshal = %q, want 👍", got)
	}

	listCtx := newModuleMessageContext(bot, chat, admin, "/reactions")
	if err := reactionsModule.listReactions(bot, listCtx); err != ext.EndGroups {
		t.Fatalf("listReactions(nil marshal) error = %v, want EndGroups", err)
	}

	checkCtx := newModuleMessageContext(bot, chat, admin, "hello")
	if err := reactionsModule.checkReactions(bot, checkCtx); err != ext.ContinueGroups {
		t.Fatalf("checkReactions(nil marshal) error = %v, want ContinueGroups", err)
	}
}

func TestLoadReactionsRegistersHelpAndHandlers(t *testing.T) {
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})
	LoadReactions(dispatcher)

	if !DefaultHelpRegistry().AbleMap[reactionsModule.moduleName] {
		t.Fatal("reactions help registration = false, want enabled")
	}
	if got := DefaultHelpRegistry().AltHelpOptions["Reactions"]; len(got) != 0 {
		t.Fatalf("reactions alt help = %v, want none", got)
	}
	if got := DefaultHelpRegistry().helpableKb["Reactions"]; len(got) != 0 {
		t.Fatalf("reactions help keyboard = %#v, want no help-only callbacks", got)
	}
}
