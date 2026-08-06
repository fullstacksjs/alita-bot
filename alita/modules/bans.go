package modules

import (
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/extraction"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

var bansModule = moduleStruct{moduleName: "Bans"}

func kickMember(b *gotgbot.Bot, chatID, userID int64) error {
	_, err := b.UnbanChatMember(chatID, userID, &gotgbot.UnbanChatMemberOpts{OnlyIfBanned: false})
	return err
}

// kickTargetValidation validates the target for kick commands.
// Checks: user in chat, not ban-protected, not the bot itself.
func kickTargetValidation(c *moderationCtx, t *target) error {
	if !chat_status.IsUserInChat(c.Bot, c.Chat, t.userID) {
		text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kick_user_not_in_chat")
		if text == "" {
			text, _ = c.Tr.GetString("common_user_not_in_chat")
		}
		_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return errUserNotInChat
	}
	if chat_status.IsUserBanProtected(c.Bot, c.Ctx, nil, t.userID) {
		text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kick_cannot_kick_admin")
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
		text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kick_is_bot_itself")
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

// kickReply builds and sends the success reply for kick commands.
func kickReply(c *moderationCtx, t *target) error {
	kickuser, err := c.Bot.GetChat(t.userID, nil)
	if err != nil {
		log.Error(err)
		return err
	}

	baseStr, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kick_kicked_user")
	if t.reason != "" {
		temp, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kick_kicked_reason")
		if temp != "" {
			baseStr += fmt.Sprintf(temp, t.reason)
		}
	}

	_, err = c.Msg.Reply(c.Bot,
		fmt.Sprintf(baseStr, formatting.MentionHtml(kickuser.Id, kickuser.FirstName)),
		formatting.Shtml(),
	)
	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

// banTargetValidation validates the target for ban commands.
// Checks: not ban-protected, not the bot itself.
func banTargetValidation(c *moderationCtx, t *target) error {
	if chat_status.IsUserBanProtected(c.Bot, c.Ctx, nil, t.userID) {
		text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_ban_is_admin")
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
		text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_ban_is_bot_itself")
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

// moderationBan is the shared moderationCommand definition for /ban.
// Handles both regular users and anonymous channels with optional durations.
func moderationBan(m *moduleStruct) *moderationCommand {
	return &moderationCommand{
		module: m,
		gates:  []gateFn{standardModGates},
		extract: func(c *moderationCtx) (target, error) {
			uid, rem := extraction.ExtractUserAndText(c.Bot, c.Ctx)
			if uid == -1 {
				return target{}, fmt.Errorf("extraction failed")
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
			isChan := chat_status.IsChannelId(uid)
			if isChan {
				return target{userID: uid, reason: rem, isChannel: true}, nil
			}
			untilDate, timeVal, reason, err := extraction.ExtractOptionalTime(c.Bot, c.Ctx, rem)
			if err != nil {
				return target{}, err
			}
			return target{userID: uid, reason: reason, timeVal: timeVal, untilDate: untilDate, isChannel: false}, nil
		},
		validate: func(c *moderationCtx, t *target) error {
			if t.isChannel {
				return nil
			}
			return banTargetValidation(c, t)
		},
		execute: func(c *moderationCtx, t *target) error {
			if t.isChannel {
				if c.Msg.ReplyToMessage != nil {
					t.userID = c.Msg.ReplyToMessage.GetSender().Id()
					_, err := c.Bot.BanChatSenderChat(c.Chat.Id, t.userID, nil)
					return err
				}
				return nil
			}
			var opts *gotgbot.BanChatMemberOpts
			if t.untilDate > 0 {
				opts = &gotgbot.BanChatMemberOpts{UntilDate: t.untilDate}
			}
			_, err := c.Chat.BanMember(c.Bot, t.userID, opts)
			return err
		},
		reply: func(c *moderationCtx, t *target) error {
			var text string
			if t.isChannel {
				if c.Msg.ReplyToMessage != nil {
					temp, _ := c.Tr.GetString("bans_anonymous_ban_user")
					text = fmt.Sprintf(temp, formatting.MentionHtml(t.userID, c.Msg.ReplyToMessage.GetSender().Name()))
				} else {
					text, _ = c.Tr.GetString("bans_anonymous_ban_reply_only")
				}
			} else {
				banUser, err := c.Bot.GetChat(t.userID, nil)
				if err != nil {
					log.Error(err)
					return err
				}
				if t.timeVal != "" {
					temp, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_ban_tban")
					text = fmt.Sprintf(temp, formatting.MentionHtml(banUser.Id, banUser.FirstName), t.timeVal)
				} else {
					temp, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_ban_normal_ban")
					text = fmt.Sprintf(temp, formatting.MentionHtml(banUser.Id, banUser.FirstName))
				}
				if t.reason != "" {
					temp, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_ban_ban_reason")
					if temp != "" {
						text += fmt.Sprintf(temp, t.reason)
					}
				}
			}
			_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return nil
		},
	}
}

// moderationKick is the shared moderationCommand definition for /kick.
func moderationKick(m *moduleStruct) *moderationCommand {
	return &moderationCommand{
		module:   m,
		gates:    []gateFn{standardModGates},
		extract:  extractFromArgs,
		validate: kickTargetValidation,
		execute: func(c *moderationCtx, t *target) error {
			return kickMember(c.Bot, c.Chat.Id, t.userID)
		},
		reply: kickReply,
	}
}

// moderationKickme is the shared moderationCommand definition for /kickme.
func moderationKickme(m *moduleStruct) *moderationCommand {
	return &moderationCommand{
		module: m,
		gates: []gateFn{
			func(c *moderationCtx) bool {
				if !chat_status.RequireGroup(c.Bot, c.Ctx, nil) {
					chat_status.NewPermissionResponder(c.Bot).Respond(c.Ctx, "chat_status_group_only_error", "", chat_status.WithReply())
					return false
				}
				if !chat_status.CanBotRestrict(c.Bot, c.Ctx, nil) {
					chat_status.NewPermissionResponder(c.Bot).Respond(c.Ctx, "chat_status_bot_restrict_group_error", "chat_status_bot_restrict_error")
					return false
				}
				return true
			},
		},
		extract: func(c *moderationCtx) (target, error) {
			if chat_status.IsUserAdmin(c.Bot, c.Chat.Id, c.User.Id) {
				text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kickme_is_admin")
				_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
				if err != nil {
					log.Error(err)
					return target{}, err
				}
				return target{}, fmt.Errorf("user is admin")
			}
			return target{userID: c.User.Id}, nil
		},
		execute: func(c *moderationCtx, t *target) error {
			return kickMember(c.Bot, c.Chat.Id, t.userID)
		},
		reply: func(c *moderationCtx, t *target) error {
			text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_kickme_ok_out")
			_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return nil
		},
	}
}

// moderationUnban is the shared moderationCommand definition for /unban.
// Supports both regular users and anonymous channels.
func moderationUnban(m *moduleStruct) *moderationCommand {
	return &moderationCommand{
		module: m,
		gates:  []gateFn{standardModGates},
		extract: func(c *moderationCtx) (target, error) {
			uid, reason := extraction.ExtractUserAndText(c.Bot, c.Ctx)
			if uid == -1 {
				return target{}, fmt.Errorf("extraction failed")
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
			return target{userID: uid, reason: reason, isChannel: chat_status.IsChannelId(uid)}, nil
		},
		validate: func(c *moderationCtx, t *target) error {
			if t.userID == c.Bot.Id {
				text, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_unban_is_bot_itself")
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
			if t.isChannel {
				if c.Msg.ReplyToMessage != nil {
					t.userID = c.Msg.ReplyToMessage.GetSender().Id()
					_, err := c.Bot.UnbanChatSenderChat(c.Chat.Id, t.userID, nil)
					return err
				}
				return nil
			}
			_, err := c.Chat.UnbanMember(c.Bot, t.userID, nil)
			return err
		},
		reply: func(c *moderationCtx, t *target) error {
			var text string
			if t.isChannel {
				if c.Msg.ReplyToMessage != nil {
					temp, _ := c.Tr.GetString("bans_anonymous_unban_user")
					text = fmt.Sprintf(temp, formatting.MentionHtml(t.userID, c.Msg.ReplyToMessage.GetSender().Name()))
				} else {
					text, _ = c.Tr.GetString("bans_anonymous_unban_reply_only")
				}
			} else {
				banUser, err := c.Bot.GetChat(t.userID, nil)
				if err != nil {
					log.Error(err)
					return err
				}
				temp, _ := c.Tr.GetString(strings.ToLower(c.Module.moduleName) + "_unban_unbanned_user")
				text = fmt.Sprintf(temp, formatting.MentionHtml(banUser.Id, banUser.FirstName))
				if t.reason != "" {
					temp, _ := c.Tr.GetString("bans_ban_ban_reason")
					if temp != "" {
						text += fmt.Sprintf(temp, t.reason)
					}
				}
			}
			_, err := c.Msg.Reply(c.Bot, text, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return nil
		},
	}
}

func (m moduleStruct) kick(b *gotgbot.Bot, ctx *ext.Context) error {
	return moderationKick(&m).run(b, ctx)
}

func (m moduleStruct) kickme(b *gotgbot.Bot, ctx *ext.Context) error {
	return moderationKickme(&m).run(b, ctx)
}

func (m moduleStruct) ban(b *gotgbot.Bot, ctx *ext.Context) error {
	return moderationBan(&m).run(b, ctx)
}

func (m moduleStruct) unban(b *gotgbot.Bot, ctx *ext.Context) error {
	return moderationUnban(&m).run(b, ctx)
}

// LoadBans registers all ban-related command handlers with the dispatcher.
func LoadBans(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[bansModule.moduleName] = true

	dispatcher.AddHandler(handlers.NewCommand("ban", bansModule.ban))
	dispatcher.AddHandler(handlers.NewCommand("unban", bansModule.unban))
	dispatcher.AddHandler(handlers.NewCommand("kick", bansModule.kick))
	dispatcher.AddHandler(handlers.NewCommand("kickme", bansModule.kickme))
}

func init() {
	RegisterLegacyModule("Bans", 70, LoadBans)
}
