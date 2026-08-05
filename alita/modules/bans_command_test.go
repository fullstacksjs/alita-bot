package modules

import (
	"errors"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func newBanReplyContext(
	bot *gotgbot.Bot,
	chat gotgbot.Chat,
	admin gotgbot.User,
	target gotgbot.User,
	text string,
) *ext.Context {
	ctx := newModuleMessageContext(bot, chat, admin, text)
	ctx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 303,
		Date:      1,
		Chat:      chat,
		From:      &target,
		Text:      "message being moderated",
	}
	return ctx
}

func TestBanReplyBansUserWithoutButton(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	ctx := newBanReplyContext(bot, chat, admin, target, "/ban spam")
	if err := bansModule.ban(bot, ctx); err != ext.EndGroups {
		t.Fatalf("ban() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("banChatMember"); len(calls) != 1 {
		t.Fatalf("banChatMember calls = %d, want 1", len(calls))
	}
	calls := client.callsFor("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
	if calls[0].Params["reply_markup"] != nil {
		t.Fatal("ban reply should not include unban button markup")
	}
}

func TestBanCommandOptionalDuration(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	// Temporary ban with duration and reason via reply
	ctx := newBanReplyContext(bot, chat, admin, target, "/ban 1h temp reason")
	if err := bansModule.ban(bot, ctx); err != ext.EndGroups {
		t.Fatalf("ban(1h temp reason) error = %v, want EndGroups", err)
	}
	calls := client.callsFor("banChatMember")
	if len(calls) != 1 {
		t.Fatalf("banChatMember calls = %d, want 1", len(calls))
	}
	if calls[0].Params["until_date"] == nil {
		t.Fatalf("banChatMember params = %#v, want until_date", calls[0].Params)
	}
}

func TestBanCommandRejectsMissingChannelAndProtectedTargets(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	tests := []struct {
		name string
		text string
	}{
		{name: "missing target", text: "/ban"},
		{name: "channel id without reply", text: "/ban -1001234567890"},
		{name: "protected service admin", text: "/ban 777000"},
		{name: "bot itself", text: "/ban 999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)
			if err := bansModule.ban(bot, ctx); err != ext.EndGroups {
				t.Fatalf("ban(%s) error = %v, want EndGroups", tt.name, err)
			}
		})
	}
	if calls := client.callsFor("banChatMember"); len(calls) != 0 {
		t.Fatalf("banChatMember calls = %d, want none for rejected targets", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != len(tests) {
		t.Fatalf("sendMessage calls = %d, want one denial per rejected target", len(calls))
	}
}

func TestBanCommandBansAnonymousChannelFromReply(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	channel := gotgbot.Chat{Id: -1001234567890, Type: "channel", Title: "Spam Channel"}
	ctx := newModuleMessageContext(bot, chat, admin, "/ban -1001234567890")
	ctx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 304,
		Date:      1,
		Chat:      chat,
		SenderChat: &gotgbot.Chat{
			Id:    channel.Id,
			Type:  channel.Type,
			Title: channel.Title,
		},
		Text: "channel post",
	}

	if err := bansModule.ban(bot, ctx); err != ext.EndGroups {
		t.Fatalf("ban(anonymous channel) error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("banChatSenderChat"); len(calls) != 1 {
		t.Fatalf("banChatSenderChat calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want anonymous ban confirmation", len(calls))
	}
}

func TestKickUsesNativeRemovalThenReply(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	ctx := newBanReplyContext(bot, chat, admin, target, "/kick too much")
	if err := bansModule.kick(bot, ctx); err != ext.EndGroups {
		t.Fatalf("kick() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("unbanChatMember"); len(calls) != 1 {
		t.Fatalf("unbanChatMember calls = %d, want 1", len(calls))
	} else if onlyIfBanned, ok := calls[0].Params["only_if_banned"].(bool); ok && onlyIfBanned {
		t.Fatalf("only_if_banned = %#v, want false so a current member is removed", calls[0].Params["only_if_banned"])
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestKickRejectsInvalidAndProtectedTargets(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, tt := range []struct {
		name string
		text string
	}{
		{name: "missing target", text: "/kick"},
		{name: "anonymous channel", text: "/kick -1001234567890"},
		{name: "telegram service admin", text: "/kick 777000"},
		{name: "bot itself", text: "/kick 999"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)
			if err := bansModule.kick(bot, ctx); err != ext.EndGroups {
				t.Fatalf("kick(%s) error = %v, want EndGroups", tt.name, err)
			}
		})
	}

	if calls := client.callsFor("unbanChatMember"); len(calls) != 0 {
		t.Fatalf("unbanChatMember calls = %d, want none for rejected kick targets", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 4 {
		t.Fatalf("sendMessage calls = %d, want one denial per rejected target", len(calls))
	}
}

func TestKickRejectsTargetNotInChat(t *testing.T) {
	client := newModuleBotClient()
	client.responses["getChatMember"] = []byte(
		`{"status":"left","user":{"id":42,"is_bot":false,"first_name":"Gone"}}`,
	)
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	ctx := newModuleMessageContext(bot, chat, admin, "/kick 42")
	if err := bansModule.kick(bot, ctx); err != ext.EndGroups {
		t.Fatalf("kick(user left) error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("unbanChatMember"); len(calls) != 0 {
		t.Fatalf("unbanChatMember calls = %d, want none for target outside chat", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want user-not-in-chat denial", len(calls))
	}
}

func TestKickMeRemovesRequesterAndReplies(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	ctx := newModuleMessageContext(bot, chat, target, "/kickme")
	if err := bansModule.kickme(bot, ctx); err != ext.EndGroups {
		t.Fatalf("kickme() error = %v, want EndGroups", err)
	}
	calls := client.callsFor("unbanChatMember")
	if len(calls) != 1 {
		t.Fatalf("unbanChatMember calls = %d, want 1", len(calls))
	}
	if got := calls[0].Params["user_id"]; got != int64(42) {
		t.Fatalf("unbanChatMember user_id = %v, want 42", got)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestKickMePropagatesGotgbotRequestErrors(t *testing.T) {
	requestErr := errors.New("telegram request failed")
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}

	for _, tt := range []struct {
		name   string
		method string
	}{
		{name: "kickme removal failure", method: "unbanChatMember"},
		{name: "kickme reply failure", method: "sendMessage"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			client.errors[tt.method] = requestErr
			ctx := newModuleMessageContext(bot, chat, target, "/kickme")

			err := bansModule.kickme(bot, ctx)
			if !errors.Is(err, requestErr) {
				t.Fatalf("kickme returned error %v, want request error", err)
			}
		})
	}
}

func TestUnbanCommandUnbansUser(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	ctx := newModuleMessageContext(bot, chat, admin, "/unban 42")
	if err := bansModule.unban(bot, ctx); err != ext.EndGroups {
		t.Fatalf("unban() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("unbanChatMember"); len(calls) != 1 {
		t.Fatalf("unbanChatMember calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestUnbanRejectsMissingAndBotTargets(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, tt := range []struct {
		name string
		text string
	}{
		{name: "missing", text: "/unban"},
		{name: "bot itself", text: "/unban 999"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newModuleMessageContext(bot, chat, admin, tt.text)
			if err := bansModule.unban(bot, ctx); err != ext.EndGroups {
				t.Fatalf("unban(%s) error = %v, want EndGroups", tt.name, err)
			}
		})
	}
	if calls := client.callsFor("unbanChatMember"); len(calls) != 0 {
		t.Fatalf("unbanChatMember calls = %d, want none for rejected unbans", len(calls))
	}
}

func TestUnbanAnonymousChannelBranches(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	noReplyCtx := newModuleMessageContext(bot, chat, admin, "/unban -1001234567890")
	if err := bansModule.unban(bot, noReplyCtx); err != ext.EndGroups {
		t.Fatalf("unban anonymous without reply error = %v, want EndGroups", err)
	}

	replyCtx := newModuleMessageContext(bot, chat, admin, "/unban -1001234567890")
	replyCtx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 304,
		Date:      1,
		Chat:      chat,
		SenderChat: &gotgbot.Chat{
			Id:    -1001234567890,
			Type:  "channel",
			Title: "Spam Channel",
		},
		Text: "channel post",
	}
	if err := bansModule.unban(bot, replyCtx); err != ext.EndGroups {
		t.Fatalf("unban anonymous reply error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("unbanChatSenderChat"); len(calls) != 1 {
		t.Fatalf("unbanChatSenderChat calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 2 {
		t.Fatalf("sendMessage calls = %d, want both anonymous branch replies", len(calls))
	}
}

func TestKickMeRejectsAdminsAndLoadBansRegistersHelp(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	ctx := newModuleMessageContext(bot, chat, admin, "/kickme")
	if err := bansModule.kickme(bot, ctx); err != ext.EndGroups {
		t.Fatalf("kickme(admin) error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("unbanChatMember"); len(calls) != 0 {
		t.Fatalf("unbanChatMember calls = %d, want none for admin kickme", len(calls))
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})
	LoadBans(dispatcher)
	if !DefaultHelpRegistry().AbleMap[bansModule.moduleName] {
		t.Fatal("bans help registration = false, want enabled")
	}
}

func TestBanCommandsPropagateGotgbotRequestErrors(t *testing.T) {
	requestErr := errors.New("telegram request failed")
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Ban Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	target := gotgbot.User{Id: 42, FirstName: "Member"}
	channel := gotgbot.Chat{Id: -1001234567890, Type: "channel", Title: "Spam Channel"}
	channelReplyContext := func(bot *gotgbot.Bot, text string) *ext.Context {
		ctx := newModuleMessageContext(bot, chat, admin, text)
		ctx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
			MessageId: 304,
			Date:      1,
			Chat:      chat,
			SenderChat: &gotgbot.Chat{
				Id:    channel.Id,
				Type:  channel.Type,
				Title: channel.Title,
			},
			Text: "channel post",
		}
		return ctx
	}

	for _, tt := range []struct {
		name   string
		method string
		text   string
		ctx    func(*gotgbot.Bot) *ext.Context
		run    func(*gotgbot.Bot, *ext.Context) error
	}{
		{
			name:   "kick member failure",
			method: "unbanChatMember",
			text:   "/kick spam",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newBanReplyContext(bot, chat, admin, target, "/kick spam") },
			run:    bansModule.kick,
		},
		{
			name:   "kick get chat failure",
			method: "getChat",
			text:   "/kick spam",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newBanReplyContext(bot, chat, admin, target, "/kick spam") },
			run:    bansModule.kick,
		},
		{
			name:   "kick send failure",
			method: "sendMessage",
			text:   "/kick spam",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newBanReplyContext(bot, chat, admin, target, "/kick spam") },
			run:    bansModule.kick,
		},
		{
			name:   "ban member failure",
			method: "banChatMember",
			text:   "/ban spam",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newBanReplyContext(bot, chat, admin, target, "/ban spam") },
			run:    bansModule.ban,
		},
		{
			name:   "ban anonymous sender failure",
			method: "banChatSenderChat",
			text:   "/ban -1001234567890",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return channelReplyContext(bot, "/ban -1001234567890") },
			run:    bansModule.ban,
		},
		{
			name:   "ban send failure",
			method: "sendMessage",
			text:   "/ban spam",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newBanReplyContext(bot, chat, admin, target, "/ban spam") },
			run:    bansModule.ban,
		},
		{
			name:   "unban member failure",
			method: "unbanChatMember",
			text:   "/unban 42",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newModuleMessageContext(bot, chat, admin, "/unban 42") },
			run:    bansModule.unban,
		},
		{
			name:   "unban get chat failure",
			method: "getChat",
			text:   "/unban 42",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newModuleMessageContext(bot, chat, admin, "/unban 42") },
			run:    bansModule.unban,
		},
		{
			name:   "unban anonymous sender failure",
			method: "unbanChatSenderChat",
			text:   "/unban -1001234567890",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return channelReplyContext(bot, "/unban -1001234567890") },
			run:    bansModule.unban,
		},
		{
			name:   "unban send failure",
			method: "sendMessage",
			text:   "/unban 42",
			ctx:    func(bot *gotgbot.Bot) *ext.Context { return newModuleMessageContext(bot, chat, admin, "/unban 42") },
			run:    bansModule.unban,
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
