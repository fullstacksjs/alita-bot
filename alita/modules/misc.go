package modules

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"

	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/config"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"

	"github.com/divkix/Alita_Robot/alita/utils/extraction"
)

var miscModule = moduleStruct{moduleName: "Misc"}

// echomsg handles the /tell command to make the bot echo a message
// as a reply to another message, requiring admin permissions.
func (moduleStruct) echomsg(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	args := ctx.Args()[1:]

	if !chat_status.RequireGroup(b, ctx, nil) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_group_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}
	if msg.From == nil || !chat_status.IsUserAdmin(b, msg.Chat.Id, msg.From.Id) {
		return ext.EndGroups
	}

	replyMsg := msg.ReplyToMessage
	if replyMsg == nil {
		tr := i18n.English()
		text, _ := tr.GetString("misc_reply_to_someone")
		_, _ = msg.Reply(b, text, nil)
		return ext.EndGroups
	}

	if len(args) > 0 {
		// Send the echo first; only delete the command on success so a failed
		// send does not destroy the admin's command with no content echoed.
		echoText := strings.Join(strings.Split(msg.OriginalHTML(), " ")[1:], " ")
		_, err := msg.Reply(b,
			echoText,
			&gotgbot.SendMessageOpts{
				ReplyParameters: &gotgbot.ReplyParameters{
					MessageId: replyMsg.MessageId,
				},
				ParseMode: formatting.Shtml().ParseMode,
			},
		)
		if err != nil {
			log.Error(err)
			// Leave the command message in place so the admin can see the echo failed.
			return ext.EndGroups
		}
		if _, derr := msg.Delete(b, nil); derr != nil {
			log.Error(derr)
		}
	} else {
		tr := i18n.English()
		text, _ := tr.GetString("misc_provide_content")
		_, _ = msg.Reply(b, text, nil)
	}

	return ext.EndGroups
}

// getId handles the /id command to display IDs of users, chats,
// files, and forwarded messages with detailed information.
func (moduleStruct) getId(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	userId := extraction.ExtractUser(b, ctx)
	if userId == -1 {
		return ext.EndGroups
	}
	var builder strings.Builder
	builder.Grow(512) // Pre-allocate capacity for better performance

	if userId != 0 {
		if msg.ReplyToMessage != nil {
			tr := i18n.English()
			temp, _ := tr.GetString("misc_chat_id")
			text := fmt.Sprintf(temp, msg.Chat.Id)
			builder.WriteString(text)
			builder.WriteString("\n")
			if msg.IsTopicMessage {
				temp2, _ := tr.GetString("misc_thread_id")
				text = fmt.Sprintf(temp2, msg.MessageThreadId)
				builder.WriteString(text)
				builder.WriteString("\n")
			}
			if msg.ReplyToMessage.From != nil {
				originalId := msg.ReplyToMessage.From.Id
				_, user1Name, _ := extraction.GetUserInfo(originalId)
				temp3, _ := tr.GetString("misc_user_id")
				text = fmt.Sprintf(temp3, user1Name, originalId)
				builder.WriteString(text)
				builder.WriteString("\n")
			}

			if rpm := msg.ReplyToMessage; rpm != nil {
				if frpm := rpm.ForwardOrigin; frpm != nil {
					if frpm.GetDate() != 0 {
						fwdd := frpm.MergeMessageOrigin()

						if fwdc := fwdd.SenderUser; fwdc != nil {
							user1Id := fwdc.Id
							_, user1Name, _ := extraction.GetUserInfo(user1Id)
							temp4, _ := tr.GetString("misc_forwarded_from_user")
							text = fmt.Sprintf(temp4, user1Name, user1Id)
							builder.WriteString(text)
							builder.WriteString("\n")
						}

						if fwdc := fwdd.Chat; fwdc != nil {
							temp5, _ := tr.GetString("misc_forwarded_from_chat")
							text = fmt.Sprintf(temp5, fwdc.Title, fwdc.Id)
							builder.WriteString(text)
							builder.WriteString("\n")
						}
					}
				}
			}
			if msg.ReplyToMessage.Animation != nil {
				temp6, _ := tr.GetString("misc_gif_id")
				text = fmt.Sprintf(temp6, msg.ReplyToMessage.Animation.FileId)
				builder.WriteString(text)
				builder.WriteString("\n")
			}
			if msg.ReplyToMessage.Sticker != nil {
				temp7, _ := tr.GetString("misc_sticker_id")
				text = fmt.Sprintf(temp7, msg.ReplyToMessage.Sticker.FileId)
				builder.WriteString(text)
				builder.WriteString("\n")
			}
		} else {
			_, name, _ := extraction.GetUserInfo(userId)
			tr := i18n.English()
			temp, _ := tr.GetString("misc_user_id_is")
			text := fmt.Sprintf(temp, name, userId)
			builder.WriteString(text)
		}
	} else {
		chat := ctx.EffectiveChat
		tr := i18n.English()
		if ctx.Message.Chat.Type == "private" {
			temp, _ := tr.GetString("misc_your_id_private")
			text := fmt.Sprintf(temp, chat.Id)
			builder.WriteString(text)
		} else {
			if msg.From == nil {
				temp, _ := tr.GetString("common_anonymous_user_error")
				builder.WriteString(temp)
			} else {
				temp, _ := tr.GetString("misc_your_id_group")
				text := fmt.Sprintf(temp, msg.From.Id, chat.Id)
				builder.WriteString(text)
			}
		}
	}

	_, err := msg.Reply(b,
		builder.String(),
		formatting.Shtml(),
	)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// ping handles the /ping command to measure bot-to-Telegram API round-trip time
func (moduleStruct) ping(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage

	tr := i18n.English()

	// Step 1: Measure sendMessage RTT (includes Telegram message processing)
	pingingText, _ := tr.GetString("misc_pinging")
	sendStart := time.Now()
	sentMsg, err := msg.Reply(b, pingingText, &gotgbot.SendMessageOpts{
		ParseMode: formatting.HTML,
	})
	sendLatency := time.Since(sendStart)
	if err != nil {
		log.WithError(err).Error("[Ping] Failed to send ping response")
		return err
	}

	// Step 2: Measure getMe RTT (lightweight call, baseline network latency)
	getMeStart := time.Now()
	_, getMeErr := b.GetMe(nil)
	getMeLatency := time.Since(getMeStart)
	if getMeErr != nil {
		log.WithError(getMeErr).Error("[Ping] Failed to call getMe")
	}

	// Step 3: Edit with detailed breakdown
	text := fmt.Sprintf(
		"🏓 <b>Pong!</b>\n\n"+
			"<b>API RTT</b> (getMe): <code>%dms</code>\n"+
			"<b>Send msg</b>: <code>%dms</code>\n"+
			"<b>Overhead</b>: <code>%dms</code>",
		getMeLatency.Milliseconds(),
		sendLatency.Milliseconds(),
		(sendLatency - getMeLatency).Milliseconds(),
	)
	var userId int64
	if msg.From != nil {
		userId = msg.From.Id
	}
	_, _, err = sentMsg.EditText(b, text, &gotgbot.EditMessageTextOpts{
		ParseMode: formatting.HTML,
	})
	if err != nil {
		log.WithError(err).Error("[Ping] Failed to edit ping response")
		return err
	}

	log.WithFields(log.Fields{
		"user_id":       userId,
		"send_latency":  sendLatency.Milliseconds(),
		"getme_latency": getMeLatency.Milliseconds(),
	}).Debug("[Ping] Response sent")

	return ext.EndGroups
}

// info handles the /info command to display detailed information
// about a user or channel including ID, name, and special roles.
func (moduleStruct) info(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	sender := ctx.EffectiveSender
	userId := extraction.ExtractUser(b, ctx)
	switch userId {
	case -1:
		return ext.EndGroups
	case 0:
		// 0 id is for self
		if sender == nil {
			return ext.EndGroups
		}
		userId = sender.Id()
	}

	username, name, found := extraction.GetUserInfo(userId)
	var text string

	if !found {
		tr := i18n.English()
		text, _ = tr.GetString("misc_user_not_found")
	} else {

		user := &gotgbot.User{
			Id:        userId,
			Username:  username,
			FirstName: name,
		}

		// If channel then this info
		if chat_status.IsChannelId(userId) {
			tr := i18n.English()
			textTemplate, _ := tr.GetString("misc_channel_info_header")
			text = fmt.Sprintf(textTemplate, userId, html.EscapeString(user.FirstName))

			if user.Username != "" {
				usernameTemplate, _ := tr.GetString("misc_username")
				text += fmt.Sprintf("\n"+usernameTemplate, user.Username)
				linkTemplate, _ := tr.GetString("misc_channel_link")
				text += fmt.Sprintf("\n"+linkTemplate, user.Username)
			}
		} else {
			tr := i18n.English()
			textTemplate, _ := tr.GetString("misc_user_info_header")
			text = fmt.Sprintf(textTemplate, userId, html.EscapeString(user.FirstName))
			if user.Username != "" {
				usernameTemplate, _ := tr.GetString("misc_username")
				text += fmt.Sprintf("\n"+usernameTemplate, user.Username)
			}
			linkTemplate, _ := tr.GetString("misc_user_link")
			text += fmt.Sprintf("\n"+linkTemplate, formatting.MentionHtml(user.Id, "link"))
			if user.Id == config.AppConfig.OwnerId {
				ownerText, _ := tr.GetString("misc_owner_info")
				text += "\n" + ownerText
			}
		}
	}

	_, err := msg.Reply(b, text, formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// stat handles the /stat command to display the total number
// of messages in the current group chat.
func (moduleStruct) stat(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	chat := ctx.EffectiveChat
	if !chat_status.RequireGroup(b, ctx, chat) {
		chat_status.NewPermissionResponder(b).Respond(ctx, "chat_status_group_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}
	tr := i18n.English()
	textTemplate, _ := tr.GetString("misc_total_messages")
	text := fmt.Sprintf(textTemplate, msg.Chat.Title, msg.MessageId+1)
	_, err := msg.Reply(b, text, nil)
	if err != nil {
		log.Error(err)
	}
	return ext.EndGroups
}

// LoadMisc registers all miscellaneous module handlers with the dispatcher,
// including utility commands for IDs, ping, translation, and stats.
func LoadMisc(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[miscModule.moduleName] = true

	dispatcher.AddHandler(handlers.NewCommand("stat", miscModule.stat))
	dispatcher.AddHandler(handlers.NewCommand("id", miscModule.getId))
	dispatcher.AddHandler(handlers.NewCommand("tell", miscModule.echomsg))
	dispatcher.AddHandler(handlers.NewCommand("ping", miscModule.ping))
	dispatcher.AddHandler(handlers.NewCommand("info", miscModule.info))
}

func init() {
	RegisterLegacyModule("Misc", 60, LoadMisc)
}
