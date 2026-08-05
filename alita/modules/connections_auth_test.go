package modules

import (
	"fmt"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/db/connections"
)

func TestCanUserConnectToChatAllowsTelegramServiceAdmins(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chatID := uniqueModuleChatID()

	allowed, denyKey := canUserConnectToChat(bot, chatID, 777000)
	if !allowed {
		t.Fatalf("canUserConnectToChat() allowed = false, denyKey = %q", denyKey)
	}
	if denyKey != "" {
		t.Fatalf("denyKey = %q, want empty for admin bypass", denyKey)
	}
}

func TestCanUserConnectToChatDeniesNonAdmin(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chatID := uniqueModuleChatID()

	allowed, denyKey := canUserConnectToChat(bot, chatID, 42)
	if allowed {
		t.Fatal("canUserConnectToChat() allowed = true, want false for non-admin")
	}
	if denyKey != "connections_is_user_connected_user_not_admin" {
		t.Fatalf("denyKey = %q, want user not admin key", denyKey)
	}
}

func TestDisconnectPrivateClearsConnection(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4242, FirstName: "Member"}
	chatID := uniqueModuleChatID()
	connections.ConnectId(user.Id, chatID)

	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}
	ctx := newModuleMessageContext(bot, privateChat, user, "/disconnect")
	if err := ConnectionsModule.disconnect(bot, ctx); err != ext.EndGroups {
		t.Fatalf("disconnect() error = %v, want EndGroups", err)
	}
	if connections.Connection(user.Id).Connected {
		t.Fatal("user remained connected after /disconnect")
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestDisconnectInGroupDoesNotClearConnection(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4243, FirstName: "Member"}
	connectedChatID := uniqueModuleChatID()
	connections.ConnectId(user.Id, connectedChatID)

	groupChat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Connections Chat"}
	ctx := newModuleMessageContext(bot, groupChat, user, "/disconnect")
	if err := ConnectionsModule.disconnect(bot, ctx); err != ext.EndGroups {
		t.Fatalf("disconnect() error = %v, want EndGroups", err)
	}
	if !connections.Connection(user.Id).Connected {
		t.Fatal("group /disconnect cleared private connection")
	}
}

func TestConnectionReportsConnectedChat(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4244, FirstName: "Member"}
	chatID := uniqueModuleChatID()
	connections.ConnectId(user.Id, chatID)

	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}
	ctx := newModuleMessageContext(bot, privateChat, user, "/connection")
	if err := ConnectionsModule.connection(bot, ctx); err != ext.EndGroups {
		t.Fatalf("connection() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("getChat"); len(calls) == 0 {
		t.Fatal("connection() did not fetch connected chat")
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestConnectionDisconnectsFormerMember(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 13, FirstName: "Former Member"}
	if err := connections.ConnectId(user.Id, uniqueModuleChatID()); err != nil {
		t.Fatalf("ConnectId() error = %v", err)
	}

	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Former Member"}
	ctx := newModuleMessageContext(bot, privateChat, user, "/connection")
	if err := ConnectionsModule.connection(bot, ctx); err != ext.EndGroups {
		t.Fatalf("connection() error = %v, want EndGroups", err)
	}
	if connections.Connection(user.Id).Connected {
		t.Fatal("former member's stale connection remained active")
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want stale-connection reply", len(calls))
	}
}

func TestConnectionMembershipLookupFailureDeniesWithoutDisconnecting(t *testing.T) {
	client := newModuleBotClient()
	client.errors["getChatMember"] = fmt.Errorf("telegram unavailable")
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 42449, FirstName: "Member"}
	if err := connections.ConnectId(user.Id, uniqueModuleChatID()); err != nil {
		t.Fatalf("ConnectId() error = %v", err)
	}

	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}
	ctx := newModuleMessageContext(bot, privateChat, user, "/connection")
	if err := ConnectionsModule.connection(bot, ctx); err != ext.EndGroups {
		t.Fatalf("connection() error = %v, want EndGroups", err)
	}
	if !connections.Connection(user.Id).Connected {
		t.Fatal("transient membership lookup failure disconnected a valid connection")
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want generic-error reply", len(calls))
	}
}

func TestConnectionReportsNotConnectedAndLookupErrors(t *testing.T) {
	user := gotgbot.User{Id: 42440, FirstName: "Member"}
	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}

	noConnClient := newModuleBotClient()
	noConnBot := newModuleTestBot(noConnClient)
	noConnCtx := newModuleMessageContext(noConnBot, privateChat, user, "/connection")
	if err := ConnectionsModule.connection(noConnBot, noConnCtx); err != ext.EndGroups {
		t.Fatalf("connection(not connected) error = %v, want EndGroups", err)
	}
	if calls := noConnClient.callsFor("getChat"); len(calls) != 0 {
		t.Fatalf("getChat calls = %d, want none without connection", len(calls))
	}
	if calls := noConnClient.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want not-connected reply", len(calls))
	}

	lookupClient := newModuleBotClient()
	lookupClient.errors["getChat"] = fmt.Errorf("telegram unavailable")
	lookupBot := newModuleTestBot(lookupClient)
	connections.ConnectId(user.Id, uniqueModuleChatID())
	lookupCtx := newModuleMessageContext(lookupBot, privateChat, user, "/connection")
	if err := ConnectionsModule.connection(lookupBot, lookupCtx); err != ext.EndGroups {
		t.Fatalf("connection(lookup error) error = %v, want EndGroups", err)
	}
	if !connections.Connection(user.Id).Connected {
		t.Fatal("transient connected-chat lookup cleared a valid connection")
	}
	if calls := lookupClient.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want generic-error reply", len(calls))
	}
}

func TestConnectInGroupRepliesWithPMNotice(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	chat := gotgbot.Chat{Id: uniqueModuleChatID(), Type: "supergroup", Title: "Connections Chat"}
	user := gotgbot.User{Id: 777000, FirstName: "Telegram"}
	ctx := newModuleMessageContext(bot, chat, user, "/connect")

	if err := ConnectionsModule.connect(bot, ctx); err != ext.EndGroups {
		t.Fatalf("connect() error = %v, want EndGroups", err)
	}
	if connections.Connection(user.Id).Connected {
		t.Fatal("group /connect should not create a direct DB connection")
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(calls))
	}
}

func TestConnectPrivateEstablishesConnectionAndHandlesMissingChat(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 777000, FirstName: "Admin"}
	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Admin"}
	t.Cleanup(func() { connections.DisconnectId(user.Id) })

	missingCtx := newModuleMessageContext(bot, privateChat, user, "/connect")
	if err := ConnectionsModule.connect(bot, missingCtx); err != ext.EndGroups {
		t.Fatalf("connect(missing chat) error = %v, want EndGroups", err)
	}
	if connections.Connection(user.Id).Connected {
		t.Fatal("missing chat id connected the user")
	}

	connectCtx := newModuleMessageContext(bot, privateChat, user, "/connect -1001")
	if err := ConnectionsModule.connect(bot, connectCtx); err != ext.EndGroups {
		t.Fatalf("connect(private) error = %v, want EndGroups", err)
	}
	if conn := connections.Connection(user.Id); !conn.Connected || conn.ChatId != -1001 {
		t.Fatalf("connection = %+v, want connected to -1001", conn)
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 2 {
		t.Fatalf("sendMessage calls = %d, want missing-chat and success replies", len(calls))
	}
}

func TestConnectionButtonsRenderAdminCommandsAndAnswerCallback(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4247, FirstName: "Member"}
	chatID := uniqueModuleChatID()
	connections.ConnectId(user.Id, chatID)

	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}
	ctx := newModuleCallbackContext(bot, privateChat, user, encodeCallbackData("connbtns", map[string]string{"t": "Admin"}))
	if err := ConnectionsModule.connectionButtons(bot, ctx); err != ext.EndGroups {
		t.Fatalf("connectionButtons() error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("editMessageText"); len(calls) != 1 {
		t.Fatalf("editMessageText calls = %d, want 1", len(calls))
	}
	if calls := client.callsFor("answerCallbackQuery"); len(calls) != 1 {
		t.Fatalf("answerCallbackQuery calls = %d, want 1", len(calls))
	}
}

func TestConnectionButtonsRenderUserAndMainViews(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 42470, FirstName: "Member"}
	chatID := uniqueModuleChatID()
	connections.ConnectId(user.Id, chatID)

	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}
	for _, data := range []string{
		encodeCallbackData("connbtns", map[string]string{"t": "User"}),
		encodeCallbackData("connbtns", map[string]string{"t": "Main"}),
	} {
		ctx := newModuleCallbackContext(bot, privateChat, user, data)
		if err := ConnectionsModule.connectionButtons(bot, ctx); err != ext.EndGroups {
			t.Fatalf("connectionButtons(%q) error = %v, want EndGroups", data, err)
		}
	}
	if calls := client.callsFor("editMessageText"); len(calls) != 2 {
		t.Fatalf("editMessageText calls = %d, want two button view edits", len(calls))
	}
	if calls := client.callsFor("answerCallbackQuery"); len(calls) != 2 {
		t.Fatalf("answerCallbackQuery calls = %d, want two callback answers", len(calls))
	}
}

func TestConnectionButtonsSkipMissingCallbackOrConnection(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 42471, FirstName: "Member"}
	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}

	messageCtx := newModuleMessageContext(bot, privateChat, user, "/connection")
	if err := ConnectionsModule.connectionButtons(bot, messageCtx); err != ext.EndGroups {
		t.Fatalf("connectionButtons(message update) error = %v, want EndGroups", err)
	}

	callbackCtx := newModuleCallbackContext(bot, privateChat, user, "connbtns.User")
	if err := ConnectionsModule.connectionButtons(bot, callbackCtx); err != ext.EndGroups {
		t.Fatalf("connectionButtons(not connected) error = %v, want EndGroups", err)
	}
	if calls := client.callsFor("editMessageText"); len(calls) != 0 {
		t.Fatalf("editMessageText calls = %d, want none for missing context/connection", len(calls))
	}
	if calls := client.callsFor("answerCallbackQuery"); len(calls) != 1 {
		t.Fatalf("answerCallbackQuery calls = %d, want not-connected callback answer", len(calls))
	}
	if calls := client.callsFor("sendMessage"); len(calls) != 0 {
		t.Fatalf("sendMessage calls = %d, want callback path without chat message", len(calls))
	}
}

func TestConnectionButtonsRejectInvalidData(t *testing.T) {
	client := newModuleBotClient()
	bot := newModuleTestBot(client)
	user := gotgbot.User{Id: 4248, FirstName: "Member"}
	privateChat := gotgbot.Chat{Id: user.Id, Type: "private", FirstName: "Member"}

	for _, data := range []string{
		"connbtns",
		encodeCallbackData("connbtns", map[string]string{"t": "crafted"}),
	} {
		ctx := newModuleCallbackContext(bot, privateChat, user, data)
		if err := ConnectionsModule.connectionButtons(bot, ctx); err != ext.EndGroups {
			t.Fatalf("connectionButtons(%q) error = %v, want EndGroups", data, err)
		}
	}
	if calls := client.callsFor("editMessageText"); len(calls) != 0 {
		t.Fatalf("editMessageText calls = %d, want 0 for invalid data", len(calls))
	}
	if calls := client.callsFor("answerCallbackQuery"); len(calls) != 2 {
		t.Fatalf("answerCallbackQuery calls = %d, want 2", len(calls))
	}
}
