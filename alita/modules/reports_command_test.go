package modules

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func newReportReplyContext(
	bot *gotgbot.Bot,
	chat gotgbot.Chat,
	reporter gotgbot.User,
	target gotgbot.User,
	text string,
) *ext.Context {
	ctx := newModuleMessageContext(bot, chat, reporter, text)
	ctx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 505,
		Date:      1,
		Chat:      chat,
		From:      &target,
		Text:      "reported message",
	}
	return ctx
}

func TestReportRequiresReplyAndSendsAdminReport(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Report Chat"}
	reporter := gotgbot.User{Id: 42, FirstName: "Reporter"}
	target := gotgbot.User{Id: 43, FirstName: "Target"}

	noReplyCtx := newModuleMessageContext(bot, chat, reporter, "/report")
	if err := reportsModule.report(bot, noReplyCtx); err != ext.EndGroups {
		t.Fatalf("report no reply error = %v, want EndGroups", err)
	}

	reportCtx := newReportReplyContext(bot, chat, reporter, target, "/report spam")
	if err := reportsModule.report(bot, reportCtx); err != ext.EndGroups {
		t.Fatalf("report reply error = %v, want EndGroups", err)
	}
	calls := client.callsFor("sendMessage")
	if len(calls) < 2 {
		t.Fatalf("sendMessage calls = %d, want validation and report messages", len(calls))
	}
	last := calls[len(calls)-1]
	if last.Params["reply_markup"] != nil {
		t.Fatal("report message should not include resolution action buttons")
	}
}

func TestReportRejectsInvalidReplyTargets(t *testing.T) {
	tests := []struct {
		name        string
		reporter    gotgbot.User
		target      *gotgbot.User
		wantReplies int
	}{
		{
			name:        "channel post target",
			reporter:    gotgbot.User{Id: 42, FirstName: "Reporter"},
			target:      nil,
			wantReplies: 1,
		},
		{
			name:        "self report",
			reporter:    gotgbot.User{Id: 42, FirstName: "Reporter"},
			target:      &gotgbot.User{Id: 42, FirstName: "Reporter"},
			wantReplies: 1,
		},
		{
			name:        "special reporter",
			reporter:    gotgbot.User{Id: 777000, FirstName: "Telegram"},
			target:      &gotgbot.User{Id: 43, FirstName: "Target"},
			wantReplies: 1,
		},
		{
			name:        "special target",
			reporter:    gotgbot.User{Id: 42, FirstName: "Reporter"},
			target:      &gotgbot.User{Id: 777000, FirstName: "Telegram"},
			wantReplies: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Report Chat"}
			ctx := newModuleMessageContext(bot, chat, tt.reporter, "/report")
			ctx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
				MessageId: 505,
				Date:      1,
				Chat:      chat,
				From:      tt.target,
				Text:      "reported message",
			}

			if err := reportsModule.report(bot, ctx); err != ext.EndGroups {
				t.Fatalf("report error = %v, want EndGroups", err)
			}
			if calls := client.callsFor("sendMessage"); len(calls) != tt.wantReplies {
				t.Fatalf("sendMessage calls = %d, want %d", len(calls), tt.wantReplies)
			}
		})
	}
}

func TestReportSkipsAdminBotAndAdminTargets(t *testing.T) {
	t.Run("admin reporter", func(t *testing.T) {
		client := newModuleBotClient()
		client.responses["getChatMember"] = []byte(
			`{"status":"administrator","user":{"id":42,"is_bot":false,"first_name":"Reporter"},"can_delete_messages":true}`,
		)
		bot := newModuleTestBot(client)
		chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Report Chat"}
		reporter := gotgbot.User{Id: 42, FirstName: "Reporter"}
		target := gotgbot.User{Id: 43, FirstName: "Target"}

		ctx := newReportReplyContext(bot, chat, reporter, target, "/report")
		if err := reportsModule.report(bot, ctx); err != ext.EndGroups {
			t.Fatalf("report(admin reporter) error = %v, want EndGroups", err)
		}
		if calls := client.callsFor("sendMessage"); len(calls) != 1 {
			t.Fatalf("sendMessage calls = %d, want admin reporter warning", len(calls))
		}
	})

	for _, tt := range []struct {
		name   string
		target gotgbot.User
	}{
		{name: "bot target", target: gotgbot.User{Id: 999, FirstName: "Alita", IsBot: true}},
		{name: "admin target", target: gotgbot.User{Id: 777000, FirstName: "Telegram"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Report Chat"}
			reporter := gotgbot.User{Id: 42, FirstName: "Reporter"}

			ctx := newReportReplyContext(bot, chat, reporter, tt.target, "/report")
			if err := reportsModule.report(bot, ctx); err != ext.EndGroups {
				t.Fatalf("report(%s) error = %v, want EndGroups", tt.name, err)
			}
			if calls := client.callsFor("sendMessage"); len(calls) != 1 {
				t.Fatalf("sendMessage calls = %d, want target warning", len(calls))
			}
		})
	}
}
