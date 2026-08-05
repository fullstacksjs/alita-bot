package modules

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/db/greetings"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/cache"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/content"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
	"github.com/divkix/Alita_Robot/alita/utils/helpers"
	"github.com/divkix/Alita_Robot/alita/utils/keyboard"
	"github.com/divkix/Alita_Robot/alita/utils/media"
)

// Concurrency limit for processing multiple new members
const (
	maxConcurrentMemberProcessing = 5 // Maximum concurrent member welcome/captcha processing
	recentJoinProcessTTL          = 5 * time.Second
)

var greetingsModule = moduleStruct{moduleName: "Greetings"}
var recentJoinProcessing sync.Map

func recentJoinProcessingKey(chatID, userID int64) string {
	return fmt.Sprintf("alita:recentJoinProcessing:%d:%d", chatID, userID)
}

func claimRecentJoinProcessing(chatID, userID int64) bool {
	key := recentJoinProcessingKey(chatID, userID)

	if rdb := cache.GetRedisClient(); rdb != nil {
		_, err := rdb.SetArgs(cache.Context, key, true, redis.SetArgs{
			Mode: "NX",
			TTL:  recentJoinProcessTTL,
		}).Result()
		if err == nil {
			// Key did not exist; we set it — claim is ours.
			return true
		}
		if errors.Is(err, redis.Nil) {
			// Key already existed; another goroutine claimed this join.
			return false
		}
		// Genuine Redis error — fall through to in-memory fallback.
		log.Debugf("[Greetings] Redis SETNX unavailable for join dedupe key %s, falling back to in-memory claim: %v", key, err)
	}

	if _, loaded := recentJoinProcessing.LoadOrStore(key, struct{}{}); loaded {
		return false
	}

	time.AfterFunc(recentJoinProcessTTL, func() {
		recentJoinProcessing.Delete(key)
	})

	return true
}

func clearRecentJoinProcessing(chatID, userID int64) {
	key := recentJoinProcessingKey(chatID, userID)

	if m := cache.GetMarshal(); m != nil {
		if err := m.Delete(cache.Context, key); err != nil {
			log.Debugf("[Greetings] Failed to clear shared join dedupe key %s: %v", key, err)
		}
	}

	recentJoinProcessing.Delete(key)
}

// welcome displays or toggles the configured welcome message.
func (moduleStruct) welcome(bot *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	// connection status
	connectedChat := chat_status.IsUserConnected(bot, ctx, true, false)
	if connectedChat == nil {
		return ext.EndGroups
	}
	ctx.EffectiveChat = connectedChat
	chat := ctx.EffectiveChat
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}
	args := ctx.Args()[1:]

	if len(args) == 0 || strings.ToLower(args[0]) == "noformat" {
		noformat := len(args) > 0 && strings.ToLower(args[0]) == "noformat"
		greetPrefs := greetings.GetGreetingSettings(chat.Id)
		if greetPrefs.WelcomeSettings == nil {
			log.Warnf("[Greetings][welcome] WelcomeSettings is nil for chat %d, using defaults", chat.Id)
			text, _ := i18n.English().GetString("greetings_welcome_not_configured")
			_, err := msg.Reply(bot, text, formatting.Shtml())
			if err != nil {
				log.Error(err)
				return err
			}
			return ext.EndGroups
		}
		greetingText := greetPrefs.WelcomeSettings.WelcomeText
		buttons := greetings.GetWelcomeButtons(chat.Id)

		tr := i18n.English()
		text, _ := tr.GetString("greetings_welcome_status")
		_, err := msg.Reply(bot, fmt.Sprintf(text,
			greetPrefs.WelcomeSettings.ShouldWelcome,
			greetPrefs.WelcomeSettings.CleanWelcome,
			greetPrefs.ShouldCleanService), formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}

		if noformat {
			greetingText += content.RevertButtons(buttons)
			_, err := media.SendGreeting(bot, ctx.EffectiveChat.Id, greetingText, greetPrefs.WelcomeSettings.FileID, greetPrefs.WelcomeSettings.WelcomeType, &gotgbot.InlineKeyboardMarkup{InlineKeyboard: nil}, ctx.EffectiveMessage.MessageThreadId)
			if err != nil {
				log.Error(err)
				return err
			}
		} else {
			greetingText, buttons = formatting.FormattingReplacer(bot, chat, user, greetingText, buttons)
			keyb := keyboard.BuildKeyboard(buttons)
			keyboard := gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyb}
			_, err := media.SendGreeting(bot, ctx.EffectiveChat.Id, greetingText, greetPrefs.WelcomeSettings.FileID, greetPrefs.WelcomeSettings.WelcomeType, &keyboard, ctx.EffectiveMessage.MessageThreadId)
			if err != nil {
				log.Error(err)
				return err
			}
		}

	} else if len(args) >= 1 {
		if !chat_status.RequireUserAdmin(bot, ctx, nil, user.Id) {
			chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_user_admin_cmd_error", "chat_status_user_admin_button_error", chat_status.WithReplyFallback())
			return ext.EndGroups
		}
		var err error
		switch strings.ToLower(args[0]) {
		case "on", "yes":
			tr := i18n.English()
			if dbErr := greetings.SetWelcomeToggle(chat.Id, true); dbErr != nil {
				log.Errorf("[Greetings] SetWelcomeToggle failed for chat %d: %v", chat.Id, dbErr)
				errText, _ := tr.GetString("common_settings_save_failed")
				_, _ = msg.Reply(bot, errText, formatting.Shtml())
				return ext.EndGroups
			}
			text, _ := tr.GetString("greetings_welcome_enabled")
			_, err = msg.Reply(bot, text, formatting.Shtml())
		case "off", "no":
			tr := i18n.English()
			if dbErr := greetings.SetWelcomeToggle(chat.Id, false); dbErr != nil {
				log.Errorf("[Greetings] SetWelcomeToggle failed for chat %d: %v", chat.Id, dbErr)
				errText, _ := tr.GetString("common_settings_save_failed")
				_, _ = msg.Reply(bot, errText, formatting.Shtml())
				return ext.EndGroups
			}
			text, _ := tr.GetString("greetings_welcome_disabled")
			_, err = msg.Reply(bot, text, formatting.Shtml())
		default:
			tr := i18n.English()
			text, _ := tr.GetString("greetings_welcome_invalid_option")
			_, err = msg.Reply(bot, text, formatting.Shtml())
		}

		if err != nil {
			log.Error(err)
			return err
		}
	}
	return ext.EndGroups
}

// setWelcome allows admins to set a custom welcome message for new chat members.
// Supports text, media, and inline buttons with formatting and placeholder variables.
func (moduleStruct) setWelcome(bot *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	// connection status
	connectedChat := chat_status.IsUserConnected(bot, ctx, true, false)
	if connectedChat == nil {
		return ext.EndGroups
	}
	ctx.EffectiveChat = connectedChat
	chat := ctx.EffectiveChat
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}

	// check permission
	if !chat_status.CanUserChangeInfo(bot, ctx, chat, user.Id) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_change_info_cmd_error", "chat_status_change_info_button_error")
		return ext.EndGroups
	}

	result := content.ExtractWelcome(msg, "welcome")
	text, dataType, content, buttons, errorMsg := result.Text, result.DataType, result.FileID, result.Buttons, result.ErrorMsg
	if dataType == -1 {
		_, err := msg.Reply(bot, errorMsg, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	tr := i18n.English()
	if dbErr := greetings.SetWelcomeText(chat.Id, text, content, buttons, dataType); dbErr != nil {
		log.Errorf("[Greetings] SetWelcomeText failed for chat %d: %v", chat.Id, dbErr)
		errText, _ := tr.GetString("common_settings_save_failed")
		_, _ = msg.Reply(bot, errText, formatting.Shtml())
		return ext.EndGroups
	}
	successText, _ := tr.GetString("greetings_welcome_set_success")
	_, err := msg.Reply(bot, successText, formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// greetingToggleConfig captures the per-command differences between the three
// near-identical on/off/show greeting toggle handlers.
type greetingToggleConfig struct {
	// getPref returns the current setting; for clean* it also emits the
	// "settings nil" warn log and returns false in that case.
	getPref func(chatID int64) bool
	// setPref persists the new setting.
	setPref func(chatID int64, val bool) error
	// saveErrLog is the setter name used in the DB-error log line.
	saveErrLog string
	// connectBotAdmin is the botAdmin arg passed to IsUserConnected.
	connectBotAdmin bool
	// Message keys for each branch.
	showEnabled  string
	showDisabled string
	setEnabled   string
	setDisabled  string
	invalid      string
	// useMarkdownOnEnabled renders the enabled "show" branch with Smarkdown
	// instead of Shtml (the delJoined quirk); disabled show + all other
	// branches always use Shtml.
	useMarkdownOnEnabled bool
}

var cleanWelcomeToggleConfig = greetingToggleConfig{
	getPref: func(chatID int64) bool {
		greetSettings := greetings.GetGreetingSettings(chatID)
		if greetSettings.WelcomeSettings == nil {
			log.Warnf("[Greetings][cleanWelcome] WelcomeSettings is nil for chat %d, using default (false)", chatID)
			return false
		}
		return greetSettings.WelcomeSettings.CleanWelcome
	},
	setPref:              greetings.SetCleanWelcomeSetting,
	saveErrLog:           "SetCleanWelcomeSetting",
	connectBotAdmin:      false,
	showEnabled:          "greetings_clean_welcome_not",
	showDisabled:         "greetings_clean_welcome_should",
	setEnabled:           "greetings_clean_welcome_enable",
	setDisabled:          "greetings_clean_welcome_disable",
	invalid:              "greetings_clean_welcome_invalid_option",
	useMarkdownOnEnabled: false,
}

var delJoinedToggleConfig = greetingToggleConfig{
	getPref: func(chatID int64) bool {
		return greetings.GetGreetingSettings(chatID).ShouldCleanService
	},
	setPref:              greetings.SetShouldCleanService,
	saveErrLog:           "SetShouldCleanService",
	connectBotAdmin:      true,
	showEnabled:          "greetings_clean_service_should",
	showDisabled:         "greetings_clean_service_not",
	setEnabled:           "greetings_clean_service_enable",
	setDisabled:          "greetings_clean_service_disable",
	invalid:              "greetings_clean_service_invalid_option",
	useMarkdownOnEnabled: true,
}

// greetingToggle is the shared skeleton for the on/off/show greeting toggle
// commands. Per-command differences are supplied via cfg.
func (moduleStruct) greetingToggle(bot *gotgbot.Bot, ctx *ext.Context, cfg greetingToggleConfig) error {
	msg := ctx.EffectiveMessage
	// connection status
	connectedChat := chat_status.IsUserConnected(bot, ctx, true, cfg.connectBotAdmin)
	if connectedChat == nil {
		return ext.EndGroups
	}
	ctx.EffectiveChat = connectedChat
	chat := ctx.EffectiveChat
	args := ctx.Args()[1:]
	var err error
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}
	// check permission
	if !chat_status.CanUserChangeInfo(bot, ctx, chat, user.Id) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_change_info_cmd_error", "chat_status_change_info_button_error")
		return ext.EndGroups
	}

	tr := i18n.English()

	if len(args) == 0 {
		if cfg.getPref(chat.Id) {
			text, _ := tr.GetString(cfg.showEnabled)
			if cfg.useMarkdownOnEnabled {
				_, err = msg.Reply(bot, text, formatting.Smarkdown())
			} else {
				_, err = msg.Reply(bot, text, formatting.Shtml())
			}
		} else {
			text, _ := tr.GetString(cfg.showDisabled)
			_, err = msg.Reply(bot, text, formatting.Shtml())
		}
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	switch strings.ToLower(args[0]) {
	case "off", "no":
		if dbErr := cfg.setPref(chat.Id, false); dbErr != nil {
			log.Errorf("[Greetings] %s failed for chat %d: %v", cfg.saveErrLog, chat.Id, dbErr)
			errText, _ := tr.GetString("common_settings_save_failed")
			_, _ = msg.Reply(bot, errText, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ := tr.GetString(cfg.setDisabled)
		_, err = msg.Reply(bot, text, formatting.Shtml())
	case "on", "yes":
		if dbErr := cfg.setPref(chat.Id, true); dbErr != nil {
			log.Errorf("[Greetings] %s failed for chat %d: %v", cfg.saveErrLog, chat.Id, dbErr)
			errText, _ := tr.GetString("common_settings_save_failed")
			_, _ = msg.Reply(bot, errText, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ := tr.GetString(cfg.setEnabled)
		_, err = msg.Reply(bot, text, formatting.Shtml())
	default:
		text, _ := tr.GetString(cfg.invalid)
		_, err = msg.Reply(bot, text, formatting.Shtml())
	}

	if err != nil {
		log.Error(err)
		return err
	}
	return ext.EndGroups
}

// cleanWelcome toggles automatic deletion of old welcome messages.
// Admins can enable/disable cleanup or check current setting. Helps keep chats tidy.
func (m moduleStruct) cleanWelcome(bot *gotgbot.Bot, ctx *ext.Context) error {
	return m.greetingToggle(bot, ctx, cleanWelcomeToggleConfig)
}

// delJoined toggles automatic deletion of service messages when users join the chat.
// Admins can enable/disable cleanup of 'user joined' messages or check current setting.
func (m moduleStruct) delJoined(bot *gotgbot.Bot, ctx *ext.Context) error {
	return m.greetingToggle(bot, ctx, delJoinedToggleConfig)
}

// SendWelcomeMessage sends the configured welcome message for a user in a chat.
func SendWelcomeMessage(bot *gotgbot.Bot, ctx *ext.Context, userID int64, firstName string) error {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[Greetings][SendWelcomeMessage] Recovered from panic: %v", r)
		}
	}()
	chat := ctx.EffectiveChat
	greetPrefs := greetings.GetGreetingSettings(chat.Id)

	// Nil check for WelcomeSettings
	if greetPrefs.WelcomeSettings == nil {
		log.Warnf("[Greetings][SendWelcomeMessage] WelcomeSettings is nil for chat %d, skipping welcome message", chat.Id)
		return nil
	}

	if greetPrefs.WelcomeSettings.ShouldWelcome {
		// Create a user object for formatting
		user := &gotgbot.User{
			Id:        userID,
			FirstName: firstName,
			IsBot:     false,
		}

		buttons := greetings.GetWelcomeButtons(chat.Id)
		res, buttons := formatting.FormattingReplacer(bot, chat, user,
			greetPrefs.WelcomeSettings.WelcomeText,
			buttons,
		)
		kb := &gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard.BuildKeyboard(buttons)}

		var threadID int64
		if ctx.EffectiveMessage != nil {
			threadID = ctx.EffectiveMessage.MessageThreadId
		}
		sent, err := media.SendGreeting(bot, chat.Id, res, greetPrefs.WelcomeSettings.FileID, greetPrefs.WelcomeSettings.WelcomeType, kb, threadID)
		if err != nil {
			log.Error(err)
			return err
		}
		if greetPrefs.WelcomeSettings.CleanWelcome {
			_ = helpers.DeleteMessageWithErrorHandling(bot, chat.Id, greetPrefs.WelcomeSettings.LastMsgId)
			if err := greetings.SetCleanWelcomeMsgId(chat.Id, sent.MessageId); err != nil {
				log.Warnf("[Greetings] Failed to store clean welcome msg ID for chat %d: %v", chat.Id, err)
			}
		}
	}
	return nil
}

func (moduleStruct) newMember(bot *gotgbot.Bot, ctx *ext.Context) error {
	newMember := ctx.ChatMember.NewChatMember.MergeChatMember().User

	if err := processSingleNewMember(bot, ctx, newMember); err != nil {
		return err
	}
	return ext.EndGroups
}

// processSingleNewMember handles a single new member joining (welcome message).
func processSingleNewMember(bot *gotgbot.Bot, ctx *ext.Context, newMember gotgbot.User) error {
	chat := ctx.EffectiveChat

	if newMember.Id == bot.Id {
		return nil
	}

	if !claimRecentJoinProcessing(chat.Id, newMember.Id) {
		log.Debugf("[Greetings][cleanService] Skipping duplicate join processing for user %d in chat %d", newMember.Id, chat.Id)
		return nil
	}

	return SendWelcomeMessage(bot, ctx, newMember.Id, newMember.FirstName)
}

// cleanService automatically deletes service messages about members joining.
// Runs when service messages are posted and deletes them if cleanup is enabled.
func (moduleStruct) cleanService(bot *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	chat := ctx.EffectiveChat
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}

	if user.Id == bot.Id {
		return ext.EndGroups
	}

	// Handle new members joining via invite links or being added
	if msg.NewChatMembers != nil {
		// Process multiple members concurrently for better performance
		numMembers := len(msg.NewChatMembers)
		if numMembers > 1 {
			// Use goroutines for multiple members
			var wg sync.WaitGroup
			// Limit concurrent processing to prevent overwhelming the API
			sem := make(chan struct{}, maxConcurrentMemberProcessing)

			for _, newMember := range msg.NewChatMembers {
				if newMember.Id == bot.Id {
					continue
				}

				wg.Add(1)
				sem <- struct{}{} // Acquire semaphore

				go func(member gotgbot.User) {
					defer wg.Done()
					defer func() { <-sem }() // Release semaphore

					if err := processSingleNewMember(bot, ctx, member); err != nil {
						log.Error(err)
					}
				}(newMember)
			}

			wg.Wait()
		} else if numMembers == 1 {
			// For single member, process directly without goroutine
			if err := processSingleNewMember(bot, ctx, msg.NewChatMembers[0]); err != nil {
				log.Error(err)
			}
		}
	}

	greetPrefs := greetings.GetGreetingSettings(chat.Id)

	if greetPrefs.ShouldCleanService {
		_, err := msg.Delete(bot, nil)
		if err != nil {
			log.Error(err)
			return err
		}
	}
	return ext.EndGroups
}

// LoadGreetings registers all greeting-related handlers with the dispatcher.
// Sets up welcome messages and service message cleanup.
func LoadGreetings(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[greetingsModule.moduleName] = true

	// Adds Formatting kb button to Greetings Menu
	DefaultHelpRegistry().helpableKb[greetingsModule.moduleName] = [][]gotgbot.InlineKeyboardButton{
		{
			{
				Text:         trS(i18n.English(), "button_formatting"),
				CallbackData: encodeCallbackData("helpq", map[string]string{"m": "Formatting"}),
			},
		},
	}

	// this is for chat member joined the chat
	dispatcher.AddHandler(
		handlers.NewChatMember(
			func(u *gotgbot.ChatMemberUpdated) bool {
				wasMember, isMember := chat_status.ExtractJoinLeftStatusChange(u)
				return !wasMember && isMember
			},
			greetingsModule.newMember,
		),
	)

	// for cleaning service messages
	dispatcher.AddHandler(
		handlers.NewMessage(
			func(msg *gotgbot.Message) bool {
				return msg.NewChatMembers != nil
			},
			greetingsModule.cleanService,
		),
	)

	dispatcher.AddHandler(handlers.NewCommand("welcome", greetingsModule.welcome))
	dispatcher.AddHandler(handlers.NewCommand("setwelcome", greetingsModule.setWelcome))
	dispatcher.AddHandler(handlers.NewCommand("cleanwelcome", greetingsModule.cleanWelcome))
	dispatcher.AddHandler(handlers.NewCommand("cleanservice", greetingsModule.delJoined))
}

func init() {
	RegisterLegacyModule("Greetings", 210, LoadGreetings)
}
