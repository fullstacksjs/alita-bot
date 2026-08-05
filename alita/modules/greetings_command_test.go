package modules

import (
	"errors"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/greetings"
)

func newGreetingMessageContext(bot *gotgbot.Bot, chat gotgbot.Chat, from gotgbot.User, text string) *ext.Context {
	return newModuleMessageContext(bot, chat, from, text)
}

func TestWelcomeToggleAndDisplay(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	for _, command := range []string{"/welcome off", "/welcome on", "/welcome maybe"} {
		ctx := newGreetingMessageContext(bot, chat, admin, command)
		if err := greetingsModule.welcome(bot, ctx); err != ext.EndGroups {
			t.Fatalf("%s error = %v, want EndGroups", command, err)
		}
	}
	if !greetings.GetGreetingSettings(chat.Id).WelcomeSettings.ShouldWelcome {
		t.Fatal("welcome toggle did not persist enabled state")
	}

	if err := greetings.SetWelcomeText(chat.Id, "Welcome {first}", "", nil, db.TEXT); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}
	ctx := newGreetingMessageContext(bot, chat, admin, "/welcome")
	if err := greetingsModule.welcome(bot, ctx); err != ext.EndGroups {
		t.Fatalf("welcome display error = %v, want EndGroups", err)
	}
	calls := client.callsFor("sendMessage")
	if len(calls) < 5 {
		t.Fatalf("sendMessage calls = %d, want toggle replies plus status and greeting", len(calls))
	}
	lastText, _ := calls[len(calls)-1].Params["text"].(string)
	if !strings.Contains(lastText, "Welcome") {
		t.Fatalf("welcome text = %q, want configured greeting", lastText)
	}
}

func TestWelcomeDisplayPropagatesMediaSendErrors(t *testing.T) {
	client := newModuleBotClient()
	client.errors["sendPhoto"] = errors.New("photo send failed")
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	if err := greetings.SetWelcomeText(chat.Id, "Welcome with photo", "photo-file", nil, db.PHOTO); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}

	ctx := newGreetingMessageContext(bot, chat, admin, "/welcome noformat")
	if err := greetingsModule.welcome(bot, ctx); err == nil {
		t.Fatal("welcome media error = nil, want sendPhoto error")
	}
	if len(client.callsFor("sendMessage")) != 1 || len(client.callsFor("sendPhoto")) != 1 {
		t.Fatal("welcome did not send status before attempting configured media")
	}
}

func TestWelcomeCommandsRequireAdmin(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	member := gotgbot.User{Id: 42, FirstName: "Member"}

	ctx := newGreetingMessageContext(bot, chat, member, "/welcome off")
	if err := greetingsModule.welcome(bot, ctx); err != ext.EndGroups {
		t.Fatalf("welcome off error = %v, want EndGroups", err)
	}
	if !greetings.GetGreetingSettings(chat.Id).WelcomeSettings.ShouldWelcome {
		t.Fatal("non-admin changed welcome setting")
	}
}

func TestSetWelcomeTextCommand(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	ctx := newGreetingMessageContext(bot, chat, admin, "/setwelcome Hello {first}")
	if err := greetingsModule.setWelcome(bot, ctx); err != ext.EndGroups {
		t.Fatalf("setWelcome error = %v, want EndGroups", err)
	}
	if got := greetings.GetGreetingSettings(chat.Id).WelcomeSettings.WelcomeText; got != "Hello {first}" {
		t.Fatalf("welcome text = %q, want command text", got)
	}

	missing := newGreetingMessageContext(bot, chat, admin, "/setwelcome")
	if err := greetingsModule.setWelcome(bot, missing); err != ext.EndGroups {
		t.Fatalf("setWelcome missing-content error = %v, want EndGroups", err)
	}
}

func TestGreetingCleanupCommands(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}

	steps := []struct {
		command string
		run     func(*gotgbot.Bot, *ext.Context) error
	}{
		{command: "/cleanwelcome on", run: greetingsModule.cleanWelcome},
		{command: "/cleanservice on", run: greetingsModule.delJoined},
		{command: "/cleanwelcome", run: greetingsModule.cleanWelcome},
		{command: "/cleanservice maybe", run: greetingsModule.delJoined},
	}
	for _, step := range steps {
		ctx := newGreetingMessageContext(bot, chat, admin, step.command)
		if err := step.run(bot, ctx); err != ext.EndGroups {
			t.Fatalf("%s error = %v, want EndGroups", step.command, err)
		}
	}
	settings := greetings.GetGreetingSettings(chat.Id)
	if !settings.WelcomeSettings.CleanWelcome || !settings.ShouldCleanService {
		t.Fatalf("cleanup settings not persisted: %#v", settings)
	}
}

func newChatMemberContext(bot *gotgbot.Bot, chat gotgbot.Chat, actor, member gotgbot.User) *ext.Context {
	update := &gotgbot.Update{
		UpdateId: 3,
		ChatMember: &gotgbot.ChatMemberUpdated{
			Chat:          chat,
			From:          actor,
			Date:          1,
			OldChatMember: gotgbot.ChatMemberLeft{User: member},
			NewChatMember: gotgbot.ChatMemberMember{User: member},
		},
	}
	return ext.NewContext(bot, update, nil)
}

func newServiceJoinContext(bot *gotgbot.Bot, chat gotgbot.Chat, from gotgbot.User, newMembers []gotgbot.User) *ext.Context {
	msg := &gotgbot.Message{
		MessageId:       301,
		Date:            1,
		Chat:            chat,
		From:            &from,
		NewChatMembers:  newMembers,
		MessageThreadId: 7,
	}
	return ext.NewContext(bot, &gotgbot.Update{UpdateId: 4, Message: msg}, nil)
}

func TestMemberJoinSendsConfiguredWelcome(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	member := gotgbot.User{Id: 4242, FirstName: "Newbie"}
	if err := greetings.SetWelcomeText(chat.Id, "Welcome {first}", "", nil, db.TEXT); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}

	clearRecentJoinProcessing(chat.Id, member.Id)
	if err := greetingsModule.newMember(bot, newChatMemberContext(bot, chat, admin, member)); err != ext.EndGroups {
		t.Fatalf("newMember error = %v, want EndGroups", err)
	}
	calls := client.callsFor("sendMessage")
	if len(calls) != 1 || !strings.Contains(calls[0].Params["text"].(string), "Welcome") {
		t.Fatalf("welcome calls = %#v", calls)
	}
}

func TestMemberJoinDeletesPreviousWelcome(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	member := gotgbot.User{Id: 4243, FirstName: "Newbie"}
	if err := greetings.SetWelcomeText(chat.Id, "Welcome {first}", "", nil, db.TEXT); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}
	if err := greetings.SetCleanWelcomeSetting(chat.Id, true); err != nil {
		t.Fatalf("SetCleanWelcomeSetting() error = %v", err)
	}
	if err := greetings.SetCleanWelcomeMsgId(chat.Id, 1234); err != nil {
		t.Fatalf("SetCleanWelcomeMsgId() error = %v", err)
	}

	clearRecentJoinProcessing(chat.Id, member.Id)
	if err := greetingsModule.newMember(bot, newChatMemberContext(bot, chat, admin, member)); err != ext.EndGroups {
		t.Fatalf("newMember error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("deleteMessage"); len(calls) != 1 {
		t.Fatalf("deleteMessage calls = %d, want previous welcome cleanup", len(calls))
	}
	if lastID := greetings.GetGreetingSettings(chat.Id).WelcomeSettings.LastMsgId; lastID == 1234 {
		t.Fatal("last welcome message ID was not updated")
	}
}

func TestMemberJoinSendsConfiguredMediaWelcome(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	member := gotgbot.User{Id: 4244, FirstName: "Newbie"}
	if err := greetings.SetWelcomeText(chat.Id, "Welcome {first}", "photo-file", nil, db.PHOTO); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}

	clearRecentJoinProcessing(chat.Id, member.Id)
	if err := greetingsModule.newMember(bot, newChatMemberContext(bot, chat, admin, member)); err != ext.EndGroups {
		t.Fatalf("newMember error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("sendPhoto"); len(calls) != 1 {
		t.Fatalf("sendPhoto calls = %d, want configured media welcome", len(calls))
	}
}

func TestCleanServiceProcessesJoinsAndDeletesServiceMessage(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	members := []gotgbot.User{{Id: 4441, FirstName: "One"}, {Id: 4442, FirstName: "Two"}}
	if err := greetings.SetWelcomeText(chat.Id, "Hello {first}", "", nil, db.TEXT); err != nil {
		t.Fatalf("SetWelcomeText() error = %v", err)
	}
	if err := greetings.SetShouldCleanService(chat.Id, true); err != nil {
		t.Fatalf("SetShouldCleanService() error = %v", err)
	}
	for _, member := range members {
		clearRecentJoinProcessing(chat.Id, member.Id)
	}

	ctx := newServiceJoinContext(bot, chat, admin, members)
	if err := greetingsModule.cleanService(bot, ctx); err != ext.EndGroups {
		t.Fatalf("cleanService error = %v, want EndGroups", err)
	}
	if len(client.callsFor("sendMessage")) != len(members) || len(client.callsFor("deleteMessage")) != 1 {
		t.Fatal("cleanService did not welcome each member and delete the service message")
	}
}

func TestProcessSingleNewMemberSkipsDuplicates(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	member := gotgbot.User{Id: 4545, FirstName: "MemberUser"}
	ctx := newServiceJoinContext(bot, chat, admin, []gotgbot.User{member})

	if err := processSingleNewMember(bot, ctx, gotgbot.User{Id: bot.Id, IsBot: true}); err != nil {
		t.Fatalf("bot join error = %v", err)
	}
	clearRecentJoinProcessing(chat.Id, member.Id)
	if !claimRecentJoinProcessing(chat.Id, member.Id) {
		t.Fatal("dedupe setup claim returned false")
	}
	if err := processSingleNewMember(bot, ctx, member); err != nil {
		t.Fatalf("duplicate member error = %v", err)
	}
	if len(client.callsFor("sendMessage")) != 0 {
		t.Fatal("bot or duplicate join sent a welcome")
	}
}

func TestGreetingCommandsPropagateTelegramErrors(t *testing.T) {
	requestErr := errors.New("telegram request failed")
	admin := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	for _, tt := range []struct {
		text string
		run  func(*gotgbot.Bot, *ext.Context) error
	}{
		{text: "/welcome", run: greetingsModule.welcome},
		{text: "/welcome on", run: greetingsModule.welcome},
		{text: "/setwelcome Hello", run: greetingsModule.setWelcome},
		{text: "/cleanwelcome", run: greetingsModule.cleanWelcome},
		{text: "/cleanservice", run: greetingsModule.delJoined},
	} {
		t.Run(tt.text, func(t *testing.T) {
			client := newModuleBotClient()
			bot := newModuleTestBot(client)
			client.errors["sendMessage"] = requestErr
			chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Greeting Chat"}
			err := tt.run(bot, newGreetingMessageContext(bot, chat, admin, tt.text))
			if !errors.Is(err, requestErr) {
				t.Fatalf("error = %v, want Telegram request error", err)
			}
		})
	}
}
