package modules

import (
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/extraction"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

var mutesModule = moduleStruct{moduleName: "Mutes"}

// muteTargetValidation validates the target for mute commands.
// Checks: user is in chat, not ban-protected, not the bot itself.
func muteTargetValidation(c *moderationCtx, t *target) error {
	if !chat_status.IsUserInChat(c.Bot, c.Chat, t.userID) {
		text, _ := c.Tr.GetString("common_user_not_in_chat")
		_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return errUserNotInChat
	}
	if chat_status.IsUserBanProtected(c.Bot, c.Ctx, nil, t.userID) {
		text, _ := c.Tr.GetString("mutes_mute_admin_error")
		if text == "" {
			text, _ = c.Tr.GetString("common_cannot_target_admin")
		}
		_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return errAdminTarget
	}
	if t.userID == c.Bot.Id {
		text, _ := c.Tr.GetString("mutes_restrict_self_error")
		if text == "" {
			text, _ = c.Tr.GetString("common_cannot_target_self")
		}
		_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return errTargetIsBot
	}
	return nil
}

// moderationMute is the shared moderationCommand definition for /mute.
// Supports optional duration specifiers and optional reason text.
func moderationMute(m *moduleStruct) *moderationCommand {
	return &moderationCommand{
		module: m,
		gates:  []gateFn{standardModGates},
		extract: func(c *moderationCtx) (target, error) {
			uid, rem := extraction.ExtractUserAndText(c.Bot, c.Ctx)
			if uid == -1 {
				return target{}, fmt.Errorf("extraction failed")
			}
			if chat_status.IsChannelId(uid) {
				text, _ := c.Tr.GetString("common_anonymous_user_error")
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return target{}, err
				}
				return target{}, fmt.Errorf("anonymous user")
			}
			if uid == 0 {
				noUserKey := "common_no_user_specified"
				text, _ := c.Tr.GetString(noUserKey)
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return target{}, err
				}
				return target{}, fmt.Errorf("no user")
			}
			untilDate, timeVal, reason, err := extraction.ExtractOptionalTime(c.Bot, c.Ctx, rem)
			if err != nil {
				return target{}, err
			}
			return target{userID: uid, reason: reason, timeVal: timeVal, untilDate: untilDate}, nil
		},
		validate: muteTargetValidation,
		execute: func(c *moderationCtx, t *target) error {
			var opts *gotgbot.RestrictChatMemberOpts
			if t.untilDate > 0 {
				opts = &gotgbot.RestrictChatMemberOpts{UntilDate: t.untilDate}
			}
			_, err := c.Chat.RestrictMember(c.Bot, t.userID, MutedPermissions, opts)
			return err
		},
		reply: func(c *moderationCtx, t *target) error {
			muteUser, err := c.Bot.GetChat(t.userID, nil)
			if err != nil {
				log.Error(err)
				return err
			}
			var baseStr string
			if t.timeVal != "" {
				temp, _ := c.Tr.GetString("mutes_tmute_message")
				baseStr = fmt.Sprintf(temp, formatting.MentionHtml(muteUser.Id, muteUser.FirstName), t.timeVal)
			} else {
				temp, _ := c.Tr.GetString("mutes_mute_message")
				baseStr = fmt.Sprintf(temp, formatting.MentionHtml(muteUser.Id, muteUser.FirstName))
			}
			if t.reason != "" {
				temp, _ := c.Tr.GetString("mutes_reason_suffix")
				baseStr += fmt.Sprintf(temp, t.reason)
			}
			_, err = c.Msg.Reply(c.Bot, baseStr, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return nil
		},
	}
}

// moderationUnmute is the shared moderationCommand definition for /unmute.
func moderationUnmute(m *moduleStruct) *moderationCommand {
	return &moderationCommand{
		module: m,
		gates:  []gateFn{standardModGates},
		extract: func(c *moderationCtx) (target, error) {
			uid, reason := extraction.ExtractUserAndText(c.Bot, c.Ctx)
			if uid == -1 {
				return target{}, fmt.Errorf("extraction failed")
			}
			if chat_status.IsChannelId(uid) {
				text, _ := c.Tr.GetString("common_anonymous_user_error")
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return target{}, err
				}
				return target{}, fmt.Errorf("anonymous user")
			}
			if uid == 0 {
				noUserKey := "common_no_user_specified"
				text, _ := c.Tr.GetString(noUserKey)
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return target{}, err
				}
				return target{}, fmt.Errorf("no user")
			}
			return target{userID: uid, reason: reason}, nil
		},
		validate: func(c *moderationCtx, t *target) error {
			if !chat_status.IsUserInChat(c.Bot, c.Chat, t.userID) {
				text, _ := c.Tr.GetString("common_user_not_in_chat")
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return err
				}
				return errUserNotInChat
			}
			if t.userID == c.Bot.Id {
				text, _ := c.Tr.GetString("mutes_restrict_self_error")
				if text == "" {
					text, _ = c.Tr.GetString("common_cannot_target_self")
				}
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return err
				}
				return errTargetIsBot
			}
			return nil
		},
		execute: func(c *moderationCtx, t *target) error {
			chat, err := c.Bot.GetChat(c.Chat.Id, nil)
			if err != nil {
				log.Error(err)
				return err
			}
			unmutePermissions := resolveUnmutePermissions(chat)
			_, err = c.Chat.RestrictMember(c.Bot, t.userID, unmutePermissions, nil)
			return err
		},
		reply: func(c *moderationCtx, t *target) error {
			muteUser, err := c.Bot.GetChat(t.userID, nil)
			if err != nil {
				log.Error(err)
				return err
			}

			temp, _ := c.Tr.GetString("mutes_unmute_message")
			baseStr := fmt.Sprintf(temp, formatting.MentionHtml(muteUser.Id, muteUser.FirstName))
			if t.reason != "" {
				temp, _ := c.Tr.GetString("mutes_reason_suffix")
				baseStr += fmt.Sprintf(temp, t.reason)
			}
			_, err = c.Msg.Reply(c.Bot, baseStr, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return nil
		},
	}
}

// mute handles the /mute command to mute a user (permanently or temporarily).
func (m moduleStruct) mute(b *gotgbot.Bot, ctx *ext.Context) error {
	return moderationMute(&m).run(b, ctx)
}

// unmute handles the /unmute command to restore chat permissions to a user.
func (m moduleStruct) unmute(b *gotgbot.Bot, ctx *ext.Context) error {
	return moderationUnmute(&m).run(b, ctx)
}

// LoadMutes registers all mute module handlers with the dispatcher.
func LoadMutes(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[mutesModule.moduleName] = true

	dispatcher.AddHandler(handlers.NewCommand("mute", mutesModule.mute))
	dispatcher.AddHandler(handlers.NewCommand("unmute", mutesModule.unmute))
}

func init() {
	RegisterLegacyModule("Mutes", 80, LoadMutes)
	RegisterAnonymousAdminHandler("mute", mutesModule.mute)
	RegisterAnonymousAdminHandler("unmute", mutesModule.unmute)
}
