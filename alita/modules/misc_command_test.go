package modules

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db/channels"
	"github.com/divkix/Alita_Robot/alita/db/user"
)

func TestPingSendsAndEditsLatencyMessage(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	ctx := newModuleMessageContext(bot, chat, user, "/ping")

	if err := miscModule.ping(bot, ctx); err != ext.EndGroups {
		t.Fatalf("ping() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("getMe"); len(calls) != 1 {
		t.Fatalf("getMe calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("editMessageText"); len(calls) != 1 {
		t.Fatalf("editMessageText calls = %d, want 1", len(calls))
	}
}

func TestStatRepliesWithGroupMessageCount(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	ctx := newModuleMessageContext(bot, chat, user, "/stat")
	ctx.EffectiveMessage.MessageId = 123

	if err := miscModule.stat(bot, ctx); err != ext.EndGroups {
		t.Fatalf("stat() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestEchoMessageRequiresReplyAndContent(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	noReplyCtx := newModuleMessageContext(bot, chat, user, "/tell hi")
	if err := miscModule.echomsg(bot, noReplyCtx); err != ext.EndGroups {
		t.Fatalf("echomsg no-reply error = %v, want EndGroups", err)
	}

	noContentCtx := newModuleMessageContext(bot, chat, user, "/tell")
	noContentCtx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 55,
		Date:      1,
		Chat:      chat,
		From:      &gotgbot.User{Id: 88, FirstName: "Target"},
		Text:      "target",
	}
	if err := miscModule.echomsg(bot, noContentCtx); err != ext.EndGroups {
		t.Fatalf("echomsg no-content error = %v, want EndGroups", err)
	}

	echoCtx := newModuleMessageContext(bot, chat, user, "/tell hello there")
	echoCtx.EffectiveMessage.ReplyToMessage = noContentCtx.EffectiveMessage.ReplyToMessage
	if err := miscModule.echomsg(bot, echoCtx); err != ext.EndGroups {
		t.Fatalf("echomsg echo error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 3 {
		t.Fatalf("sendMessage calls = %d, want 3", len(calls))
	}
	if calls := client.callsFor("deleteMessage"); len(calls) != 1 {
		t.Fatalf("deleteMessage calls = %d, want 1 for successful echo", len(calls))
	}
}

func TestGetIdRepliesForCurrentGroupUserAndReply(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}

	groupCtx := newModuleMessageContext(bot, chat, user, "/id")
	if err := miscModule.getId(bot, groupCtx); err != ext.EndGroups {
		t.Fatalf("getId group error = %v, want EndGroups", err)
	}

	replyCtx := newModuleMessageContext(bot, chat, user, "/id")
	replyCtx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 55,
		Date:      1,
		Chat:      chat,
		From:      &gotgbot.User{Id: 88, FirstName: "Target"},
		Text:      "target",
		Sticker:   &gotgbot.Sticker{FileId: "sticker-file-id"},
	}
	if err := miscModule.getId(bot, replyCtx); err != ext.EndGroups {
		t.Fatalf("getId reply error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 2 {
		t.Fatalf("sendMessage calls = %d, want 2", len(calls))
	}
}

func TestGetIdHandlesPrivateAnonymousAndMediaReply(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 42, FirstName: "Member"}

	privateChat := gotgbot.Chat{Id: 42, Type: "private", FirstName: "Member"}
	privateCtx := newModuleMessageContext(bot, privateChat, user, "/id")
	if err := miscModule.getId(bot, privateCtx); err != ext.EndGroups {
		t.Fatalf("getId private error = %v, want EndGroups", err)
	}

	groupChat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	anonymousCtx := newModuleMessageContext(bot, groupChat, user, "/id")
	anonymousCtx.EffectiveMessage.From = nil
	if err := miscModule.getId(bot, anonymousCtx); err != ext.EndGroups {
		t.Fatalf("getId anonymous error = %v, want EndGroups", err)
	}

	replyCtx := newModuleMessageContext(bot, groupChat, user, "/id")
	replyCtx.EffectiveMessage.IsTopicMessage = true
	replyCtx.EffectiveMessage.MessageThreadId = 77
	replyCtx.EffectiveMessage.ReplyToMessage = &gotgbot.Message{
		MessageId: 55,
		Date:      1,
		Chat:      groupChat,
		From:      &gotgbot.User{Id: 88, FirstName: "Target"},
		Text:      "target",
		Animation: &gotgbot.Animation{FileId: "gif-file-id"},
	}
	if err := miscModule.getId(bot, replyCtx); err != ext.EndGroups {
		t.Fatalf("getId media reply error = %v, want EndGroups", err)
	}

	if calls := client.callsFor("sendMessage"); len(calls) != 3 {
		t.Fatalf("sendMessage calls = %d, want 3", len(calls))
	}
}

func TestInfoRepliesForUnknownNumericUser(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	ctx := newModuleMessageContext(bot, chat, user, "/info 123456789")

	if err := miscModule.info(bot, ctx); err != ext.EndGroups {
		t.Fatalf("info() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestInfoRepliesForKnownOwner(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	u := gotgbot.User{Id: 42, FirstName: "Member"}
	requireUserID := time.Now().UnixNano()

	if err := user.EnsureUserInDb(requireUserID, "knownuser", "Known User"); err != nil {
		t.Fatalf("EnsureUserInDb() error = %v", err)
	}
	previousOwnerID := config.AppConfig.OwnerId
	config.AppConfig.OwnerId = requireUserID
	t.Cleanup(func() {
		config.AppConfig.OwnerId = previousOwnerID
	})

	ctx := newModuleMessageContext(bot, chat, u, "/info "+strconv.FormatInt(requireUserID, 10))

	if err := miscModule.info(bot, ctx); err != ext.EndGroups {
		t.Fatalf("info() error = %v, want EndGroups", err)
	}

	calls := client.callsFor("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
	text := calls[0].Params["text"].(string)
	for _, want := range []string{"knownuser", "Known User", strconv.FormatInt(requireUserID, 10)} {
		if !strings.Contains(text, want) {
			t.Fatalf("info text %q missing %q", text, want)
		}
	}
}

func TestInfoRepliesForKnownChannel(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Misc Chat"}
	user := gotgbot.User{Id: 42, FirstName: "Member"}
	channelID := int64(-1001234567890)
	if err := channels.UpdateChannel(channelID, "News Channel", "newsroom"); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}

	ctx := newModuleMessageContext(bot, chat, user, "/info "+strconv.FormatInt(channelID, 10))

	if err := miscModule.info(bot, ctx); err != ext.EndGroups {
		t.Fatalf("info() error = %v, want EndGroups", err)
	}

	calls := client.callsFor("sendMessage")
	if len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
	text := calls[0].Params["text"].(string)
	for _, want := range []string{"News Channel", "newsroom", strconv.FormatInt(channelID, 10)} {
		if !strings.Contains(text, want) {
			t.Fatalf("info text %q missing %q", text, want)
		}
	}
}

func TestLoadMiscRegistersHelpAndHandlers(t *testing.T) {
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{MaxRoutines: -1})
	LoadMisc(dispatcher)

	if !DefaultHelpRegistry().AbleMap[miscModule.moduleName] {
		t.Fatal("misc help registration = false, want enabled")
	}
}
