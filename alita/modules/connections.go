package modules

import (
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	log "github.com/sirupsen/logrus"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"

	"github.com/divkix/Alita_Robot/alita/db/connections"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/extraction"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
	"github.com/divkix/Alita_Robot/alita/utils/keyboard"
)

var ConnectionsModule = moduleStruct{moduleName: "Connections"}

/*
	Check the status of connection of a user in their PM

User can check if they are connected to a chat and can also bring up the keyboard for it.
Normal use will have just one option with 'User Commands' and admin will have "Admin Commands" along the earlier as
well.
*/
// connection handles the /connection command to check user's connection status.
// Shows current connected chat and provides keyboard with available commands.
func (m moduleStruct) connection(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	tr := i18n.English()

	// permission checks
	if !chat_status.RequirePrivate(b, ctx, nil) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_pm_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}

	chat := chat_status.IsUserConnected(b, ctx, false, false)
	if chat == nil {
		return ext.EndGroups
	}

	temp, _ := tr.GetString(strings.ToLower(m.moduleName) + "_connected")
	_text := fmt.Sprintf(temp, chat.Title)
	connKeyboard := keyboard.InitButtons(b, chat.Id, user.Id)
	_, err := msg.Reply(b,
		_text,
		&gotgbot.SendMessageOpts{
			ReplyMarkup: connKeyboard,
			ParseMode:   formatting.HTML,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

/*
	Connect to a chat

Use this command to connect to your chat!

Only group admins can use this command.
*/
// connect handles the /connect command to establish connection to a chat.
// Allows group administrators to remotely manage chats through private messages.
func (m moduleStruct) connect(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveChat
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	tr := i18n.English()
	var text string
	var replyMarkup gotgbot.ReplyMarkup

	if ctx.Message.Chat.Type == "private" {
		chat := extraction.ExtractChat(b, ctx)
		if chat == nil {
			return ext.EndGroups
		}

		if allowed, denyKey := canUserConnectToChat(b, chat.Id, user.Id); !allowed {
			text, _ = tr.GetString(denyKey)
		} else if err := connections.ConnectId(user.Id, chat.Id); err != nil {
			text, _ = tr.GetString("common_settings_save_failed")
		} else {
			temp, _ := tr.GetString(strings.ToLower(m.moduleName) + "_connect_connected")
			text = fmt.Sprintf(temp, chat.Title)
			replyMarkup = keyboard.InitButtons(b, chat.Id, user.Id)
		}
	} else {
		if allowed, denyKey := canUserConnectToChat(b, chat.Id, user.Id); !allowed {
			text, _ = tr.GetString(denyKey)
		} else {
			text, _ = tr.GetString(strings.ToLower(m.moduleName) + "_disconnect_need_pm")
		}
	}

	_, err := msg.Reply(b,
		text,
		&gotgbot.SendMessageOpts{
			ReplyMarkup: replyMarkup,
			ParseMode:   formatting.HTML,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// Handler for Connection buttons
// connectionButtons handles inline keyboard callbacks for connection management.
// Processes admin and user command list requests from connection interface.
func (m moduleStruct) connectionButtons(b *gotgbot.Bot, ctx *ext.Context) error {
	query, ok := callbackQueryFromContext(ctx)
	if !ok {
		return ext.EndGroups
	}
	user := query.From
	msg := query.Message
	tr := i18n.English()
	if msg == nil {
		text, _ := tr.GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}

	userType := ""
	if decoded, ok := decodeCallbackData(query.Data, "connbtns"); ok {
		userType, _ = decoded.Field("t")
	}
	switch userType {
	case "Admin", "User", "Main":
	default:
		log.Warnf("[Connections] Invalid callback data format: %s", query.Data)
		text, _ := tr.GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}

	backText, _ := tr.GetString("button_back")
	var (
		replyText string
		replyKb   = gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         backText,
						CallbackData: encodeCallbackData("connbtns", map[string]string{"t": "Main"}),
					},
				},
			},
		}
	)

	chat := chat_status.IsUserConnected(b, ctx, false, false)
	if chat == nil {
		return ext.EndGroups
	}

	switch userType {
	case "Admin":
		replyText, _ = tr.GetString(strings.ToLower(m.moduleName) + "_connections_btns_admin_conn_cmds")
	case "User":
		replyText, _ = tr.GetString(strings.ToLower(m.moduleName) + "_connections_btns_user_conn_cmds")
	case "Main":
		temp, _ := tr.GetString(strings.ToLower(m.moduleName) + "_connected")
		replyText = fmt.Sprintf(temp, chat.Title)
		replyKb = keyboard.InitButtons(b, chat.Id, user.Id)
	}

	_, _, err := msg.EditText(b,
		replyText,
		&gotgbot.EditMessageTextOpts{
			ReplyMarkup: replyKb,
			ParseMode:   formatting.HTML,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}

	_, err = query.Answer(b, nil)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

/*
	Disconnect from a chat

Used to disconnect from currently connected chat
*/
// disconnect handles the /disconnect command to end current chat connection.
// Removes the user's connection to allow connecting to different chats.
func (m moduleStruct) disconnect(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	tr := i18n.English()

	var text string

	if ctx.Message.Chat.Type == "private" {
		if connections.Connection(user.Id).Connected {
			if err := connections.DisconnectId(user.Id); err != nil {
				text, _ = tr.GetString("common_settings_save_failed")
			} else {
				text, _ = tr.GetString(strings.ToLower(m.moduleName) + "_disconnect_disconnected")
			}
		} else {
			text, _ = tr.GetString(strings.ToLower(m.moduleName) + "_not_connected")
		}
	} else {
		text, _ = tr.GetString(strings.ToLower(m.moduleName) + "_disconnect_need_pm")
	}

	_, err := msg.Reply(b, text, formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// LoadConnections registers all connection module handlers with the dispatcher.
// Sets up commands for managing remote chat connections and their callbacks.
func LoadConnections(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[ConnectionsModule.moduleName] = true

	dispatcher.AddHandler(handlers.NewCommand("connect", ConnectionsModule.connect))
	dispatcher.AddHandler(handlers.NewCommand("disconnect", ConnectionsModule.disconnect))
	dispatcher.AddHandler(handlers.NewCommand("connection", ConnectionsModule.connection))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("connbtns"), ConnectionsModule.connectionButtons))
}

func init() {
	RegisterLegacyModule("Connections", 170, LoadConnections)
}
