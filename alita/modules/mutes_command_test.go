package modules

import (
	"errors"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func newMuteReplyContext(
	bot *gotgbot.Bot,
	chat gotgbot.Chat,
	admin gotgbot.User,
	target gotgbot.User,
	text string,
) *ext.Context {
	ctx := newModuleMessageContext(bot, chat, admin, text)
	ctx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 404,
		Date:      1,
		Chat:      chat,
		From:      &target,
		Text:      "message being muted",
	}
	return ctx
}

func TestMuteReplyRestrictsUserWithoutButton(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	ctx := newMuteReplyContext(bot, chat, admin, target, "/mute noisy")
	if err := mutesModule.mute(bot, ctx); err != ext.EndGroups {
		t.Fatalf("mute error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("restrictChatMember"); len(calls) != 1 {
		t.Fatalf("restrictChatMember calls = %d, want 1", len(calls))
	}
	calls := client.callsFor("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want mute confirmation", len(calls))
	}
	if calls[0].Params["reply_markup"] != nil {
		t.Fatal("mute confirmation should not include unmute button")
	}
}

func TestMuteCommandOptionalDuration(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	ctx := newMuteReplyContext(bot, chat, admin, target, "/mute 1h temporary")
	if err := mutesModule.mute(bot, ctx); err != ext.EndGroups {
		t.Fatalf("mute error = %v, want EndGroups", err)
	}
	calls := client.callsFor("restrictChatMember")
	if len(calls) != 1 {
		t.Fatalf("restrictChatMember calls = %d, want 1", len(calls))
	}
	if calls[0].Params["until_date"] == nil {
		t.Fatalf("restrictChatMember params = %#v, want until_date", calls[0].Params)
	}
}

func TestMuteCommandRejectsMissingChannelAndProtectedTargets(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	tests := []struct {
		name string
		text string
	}{
		{name: "missing target", text: "/mute"},
		{name: "channel id", text: "/mute -1001234567890"},
		{name: "protected service admin", text: "/mute 777000"},
		{name: "bot itself", text: "/mute 999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)
			if err := mutesModule.mute(bot, ctx); err != ext.EndGroups {
				t.Fatalf("mute(%s) error = %v, want EndGroups", tt.name, err)
			}
		})
	}
	if calls := client.callsFor("restrictChatMember"); len(calls) != 0 {
		t.Fatalf("restrictChatMember calls = %d, want none for rejected targets", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != len(tests) {
		t.Fatalf("sendMessage calls = %d, want one denial per rejected target", len(calls))
	}
}

func TestUnmuteCommandRestoresPermissions(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	ctx := newModuleMessageContext(bot, chat, admin, "/unmute 42")
	if err := mutesModule.unmute(bot, ctx); err != ext.EndGroups {
		t.Fatalf("unmute error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("restrictChatMember"); len(calls) != 1 {
		t.Fatalf("restrictChatMember calls = %d, want unmute restriction", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want unmute confirmation", len(calls))
	}
}

func TestUnmuteRejectsMissingChannelAndSelfTargets(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, tt := range []struct {
		name string
		text string
	}{
		{name: "missing target", text: "/unmute"},
		{name: "channel id", text: "/unmute -1001234567890"},
		{name: "bot itself", text: "/unmute 999"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)
			if err := mutesModule.unmute(bot, ctx); err != ext.EndGroups {
				t.Fatalf("unmute(%s) error = %v, want EndGroups", tt.name, err)
			}
		})
	}

	if calls := client.callsFor("restrictChatMember"); len(calls) != 0 {
		t.Fatalf("restrictChatMember calls = %d, want none for rejected unmute targets", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 3 {
		t.Fatalf("sendMessage calls = %d, want one denial per rejected target", len(calls))
	}
}

func TestUnmuteRejectsAbsentTarget(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	ctx := newModuleMessageContext(bot, chat, admin, "/unmute 13")
	if err := mutesModule.unmute(bot, ctx); err != ext.EndGroups {
		t.Fatalf("unmute(absent target) error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("restrictChatMember"); len(calls) != 0 {
		t.Fatalf("restrictChatMember calls = %d, want none for absent target", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want user-not-in-chat denial", len(calls))
	}
}

func TestMuteCommandsPropagateGotgbotRequestErrors(t *testing.T) {
	requestErr := errors.New("telegram request failed")
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Mute Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	for _, tt := range []struct {
		name   string
		method string
		text   string
		ctx    func(*gotgbot.Bot) *ext.Context
		run    func(*gotgbot.Bot, *ext.Context) error
	}{
		{
			name:   "mute restrict failure",
			method: "restrictChatMember",
			text:   "/mute noisy",
			ctx: func(bot *gotgbot.Bot) *ext.Context {
				return newMuteReplyContext(bot, chat, admin, target, "/mute noisy")
			},
			run: mutesModule.mute,
		},
		{
			name:   "mute get chat failure",
			method: "getChat",
			text:   "/mute noisy",
			ctx: func(bot *gotgbot.Bot) *ext.Context {
				return newMuteReplyContext(bot, chat, admin, target, "/mute noisy")
			},
			run: mutesModule.mute,
		},
		{
			name:   "mute send failure",
			method: "sendMessage",
			text:   "/mute noisy",
			ctx: func(bot *gotgbot.Bot) *ext.Context {
				return newMuteReplyContext(bot, chat, admin, target, "/mute noisy")
			},
			run: mutesModule.mute,
		},
		{
			name:   "unmute chat lookup failure",
			method: "getChat",
			text:   "/unmute 42",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newModuleMessageContext(bot, chat, admin, "/unmute 42") },
			run:    mutesModule.unmute,
		},
		{
			name:   "unmute restrict failure",
			method: "restrictChatMember",
			text:   "/unmute 42",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newModuleMessageContext(bot, chat, admin, "/unmute 42") },
			run:    mutesModule.unmute,
		},
		{
			name:   "unmute send failure",
			method: "sendMessage",
			text:   "/unmute 42",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newModuleMessageContext(bot, chat, admin, "/unmute 42") },
			run:    mutesModule.unmute,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			client.errors[tt.method] = requestErr

			err := tt.run(bot, tt.ctx(bot))
			if !errors.Is(err, requestErr) {
				t.Fatalf("%s returned error %v, want request error", tt.text, err)
			}
		})
	}
}
