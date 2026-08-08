package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/testsqlite"
)

type moduleBotCall struct {
	Method string
	Params map[string]any
}

type moduleBotClient struct {
	mu        sync.Mutex
	calls     []moduleBotCall
	responses map[string]json.RawMessage
	errors    map[string]error
}

func newModuleTestBot(client *moduleBotClient) *gotgbot.Bot {
	return &gotgbot.Bot{
		Token:     "999:test",
		BotClient: client,
		User: gotgbot.User{
			Id:       999,
			IsBot:    true,
			Username: "AlitaTestBot",
		},
	}
}

func newModuleBotClient() *moduleBotClient {
	return &moduleBotClient{
		responses: map[string]json.RawMessage{
			"sendMessage": json.RawMessage(
				`{"message_id":9001,"date":1,"chat":{"id":-1001,"type":"supergroup","title":"Test Chat"}}`,
			),
			"editMessageText": json.RawMessage(
				`{"message_id":9001,"date":1,"chat":{"id":-1001,"type":"supergroup","title":"Test Chat"}}`,
			),
			"deleteMessage":          json.RawMessage(`true`),
			"banChatMember":          json.RawMessage(`true`),
			"banChatSenderChat":      json.RawMessage(`true`),
			"restrictChatMember":     json.RawMessage(`true`),
			"unbanChatMember":        json.RawMessage(`true`),
			"unbanChatSenderChat":    json.RawMessage(`true`),
			"leaveChat":              json.RawMessage(`true`),
			"approveChatJoinRequest": json.RawMessage(`true`),
			"declineChatJoinRequest": json.RawMessage(`true`),
			"pinChatMessage":         json.RawMessage(`true`),
			"unpinChatMessage":       json.RawMessage(`true`),
			"unpinAllChatMessages":   json.RawMessage(`true`),
			"getChatMemberCount":     json.RawMessage(`17`),
			"sendDocument": json.RawMessage(
				`{"message_id":9002,"date":1,"chat":{"id":-1001,"type":"supergroup","title":"Test Chat"},"document":{"file_id":"doc-1","file_unique_id":"doc-u1","file_name":"chatlist.txt"}}`,
			),
			"sendPhoto": json.RawMessage(
				`{"message_id":9003,"date":1,"chat":{"id":-1001,"type":"supergroup","title":"Test Chat"},"photo":[{"file_id":"photo-1","file_unique_id":"photo-u1","width":160,"height":80}]}`,
			),
			"answerCallbackQuery": json.RawMessage(`true`),
			"getMe": json.RawMessage(
				`{"id":999,"is_bot":true,"first_name":"Alita","username":"AlitaTestBot"}`,
			),
			"getChat": json.RawMessage(
				`{"id":-1001,"type":"supergroup","title":"Test Chat"}`,
			),
			"getChatMember": json.RawMessage(
				`{"status":"member","user":{"id":42,"is_bot":false,"first_name":"Member"}}`,
			),
			"getChatAdministrators": json.RawMessage(
				`[{"status":"administrator","user":{"id":999,"is_bot":true,"first_name":"Alita"},"can_pin_messages":true,"can_delete_messages":true,"can_restrict_members":true,"can_promote_members":true,"can_change_info":true,"can_invite_users":true,"can_manage_chat":true}]`,
			),
		},
		errors: make(map[string]error),
	}
}

func (c *moduleBotClient) RequestWithContext(_ context.Context, _ string, method string, params map[string]any, _ *gotgbot.RequestOpts) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	copied := make(map[string]any, len(params))
	for key, value := range params {
		copied[key] = value
	}
	c.calls = append(c.calls, moduleBotCall{Method: method, Params: copied})

	if err := c.errors[method]; err != nil {
		return nil, err
	}
	if method == "getChatMember" && fmt.Sprint(params["user_id"]) == "999" {
		return json.RawMessage(
			`{"status":"administrator","user":{"id":999,"is_bot":true,"first_name":"Alita"},"can_pin_messages":true,"can_delete_messages":true,"can_restrict_members":true,"can_promote_members":true,"can_change_info":true,"can_invite_users":true,"can_manage_chat":true}`,
		), nil
	}
	if method == "getChatMember" && fmt.Sprint(params["user_id"]) == "777000" {
		return json.RawMessage(
			`{"status":"creator","user":{"id":777000,"is_bot":false,"first_name":"Telegram"}}`,
		), nil
	}
	if method == "getChatMember" && fmt.Sprint(params["user_id"]) == "13" {
		return json.RawMessage(
			`{"status":"left","user":{"id":13,"is_bot":false,"first_name":"Left User"}}`,
		), nil
	}
	if method == "getChatMember" && fmt.Sprint(params["user_id"]) == "14" {
		return json.RawMessage(
			`{"status":"kicked","user":{"id":14,"is_bot":false,"first_name":"Kicked User"}}`,
		), nil
	}
	if response, ok := c.responses[method]; ok {
		return response, nil
	}
	return json.RawMessage(`true`), nil
}

func (c *moduleBotClient) GetAPIURL(*gotgbot.RequestOpts) string {
	return "https://api.telegram.org"
}

func (c *moduleBotClient) FileURL(token string, path string, _ *gotgbot.RequestOpts) string {
	return "https://api.telegram.org/file/bot" + token + "/" + path
}

func (c *moduleBotClient) callsFor(method string) []moduleBotCall {
	c.mu.Lock()
	defer c.mu.Unlock()

	var calls []moduleBotCall
	for _, call := range c.calls {
		if call.Method == method {
			calls = append(calls, call)
		}
	}
	return calls
}

func newModuleMessageContext(bot *gotgbot.Bot, chat gotgbot.Chat, from gotgbot.User, text string) *ext.Context {
	msg := &gotgbot.Message{
		MessageId: 101,
		Date:      1,
		Chat:      chat,
		From:      &from,
		Text:      text,
	}
	return ext.NewContext(bot, &gotgbot.Update{UpdateId: 1, Message: msg}, nil)
}

func newModuleCallbackContext(
	bot *gotgbot.Bot,
	chat gotgbot.Chat,
	from gotgbot.User,
	data string,
) *ext.Context {
	msg := &gotgbot.Message{
		MessageId: 102,
		Date:      1,
		Chat:      chat,
		From:      &from,
		Text:      "callback source",
	}
	// Match gotgbot's JSON decoder, which stores Message as a value.
	query := &gotgbot.CallbackQuery{
		Id:           "callback-1",
		From:         from,
		Message:      *msg,
		Data:         data,
		ChatInstance: "test-chat-instance",
	}
	return ext.NewContext(bot, &gotgbot.Update{UpdateId: 2, CallbackQuery: query}, nil)
}

func TestMain(m *testing.M) {
	var cleanup func()
	if db.DB == nil {
		db.DB, cleanup = testsqlite.MustOpen()
	}

	exitCode := m.Run()

	if cleanup != nil {
		cleanup()
	}

	os.Exit(exitCode)
}
