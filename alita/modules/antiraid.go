package modules

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	tgmd2html "github.com/PaulSonOfLars/gotg_md2html"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/redis/go-redis/v9"

	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/antiraid"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/cache"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/error_handling"
	"github.com/divkix/Alita_Robot/alita/utils/extraction"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

const (
	antiraidJoinWindowSeconds = 60
	antiraidPollInterval      = 30 * time.Second
	antiraidJoinsKey          = "alita:antiraid:joins" // format: joins:chat_id (sorted set)
	maxAntiRaidDuration       = antiraid.MaxRaidDuration
)

var (
	antiRaidModule = antiRaidStruct{
		moduleStruct: moduleStruct{moduleName: "AntiRaid", handlerGroup: -5},
	}
	antiRaidCtx      context.Context
	antiRaidCancel   context.CancelFunc
	antiRaidPollerMu sync.Mutex
	antiRaidPollerWG sync.WaitGroup
)

type antiRaidStruct struct {
	moduleStruct
}

// StartAntiRaidExpiryPoller starts the background expiry poller once the
// database is available. The raid window lives in SQLite, so the poller
// recovers raids that were opened before the last restart.
func StartAntiRaidExpiryPoller() {
	if db.DB == nil {
		log.Warn("[AntiRaid] Database not available, skipping expiry poller start")
		return
	}
	antiRaidPollerMu.Lock()
	defer antiRaidPollerMu.Unlock()
	if antiRaidCancel != nil {
		// Already started
		return
	}
	antiRaidCtx, antiRaidCancel = context.WithCancel(context.Background())
	antiRaidPollerWG.Add(1)
	go func(ctx context.Context) {
		defer antiRaidPollerWG.Done()
		defer error_handling.RecoverFromPanic("antiRaidExpiryPoller", "antiraid")
		antiRaidModule.expiryPoller(ctx)
	}(antiRaidCtx)
}

// StopAntiRaidExpiryPoller stops and joins the background expiry poller.
func StopAntiRaidExpiryPoller() {
	antiRaidPollerMu.Lock()
	defer antiRaidPollerMu.Unlock()
	if antiRaidCancel != nil {
		antiRaidCancel()
		antiRaidPollerWG.Wait()
		antiRaidCancel = nil
		antiRaidCtx = nil
	}
}

func joinsKey(chatID int64) string {
	return fmt.Sprintf("%s:%d", antiraidJoinsKey, chatID)
}

func trackJoin(chatID, userID int64) (count int, err error) {
	if !cache.IsRedisAvailable() {
		return 0, fmt.Errorf("cache not initialized")
	}
	now := time.Now().Unix()
	ctx := cache.Context
	rdb := cache.GetRedisClient()
	_, err = rdb.ZAdd(ctx, joinsKey(chatID), redis.Z{Score: float64(now), Member: strconv.FormatInt(userID, 10)}).Result()
	if err != nil {
		return 0, err
	}
	if err := rdb.Expire(ctx, joinsKey(chatID), time.Duration(antiraidJoinWindowSeconds)*time.Second).Err(); err != nil {
		log.WithError(err).Warnf("[AntiRaid] Failed to expire join tracking for chat %d", chatID)
	}
	_, err = rdb.ZRemRangeByScore(ctx, joinsKey(chatID), "0", strconv.FormatInt(now-int64(antiraidJoinWindowSeconds), 10)).Result()
	if err != nil {
		log.WithError(err).Warnf("[AntiRaid] ZRemRangeByScore failed on joinsKey %d", chatID)
	}
	rawCount, err := rdb.ZCard(ctx, joinsKey(chatID)).Result()
	return int(rawCount), err
}

func clearJoinTracking(chatID int64) {
	if !cache.IsRedisAvailable() {
		return
	}
	ctx := cache.Context
	rdb := cache.GetRedisClient()
	_ = rdb.Del(ctx, joinsKey(chatID)).Err()
}

// getRaidState reads the persisted raid window for a chat.
func getRaidState(chatID int64) *antiraid.RaidState {
	return antiraid.GetRaidState(chatID)
}

func (a *antiRaidStruct) expiryPoller(ctx context.Context) {
	ticker := time.NewTicker(antiraidPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.checkExpiredRaids(ctx)
		case <-ctx.Done():
			log.Info("AntiRaid expiry poller shutting down gracefully")
			return
		}
	}
}

func (a *antiRaidStruct) checkExpiredRaids(_ context.Context) {
	if db.DB == nil {
		return
	}

	expired, err := antiraid.ExpireRaids(time.Now())
	if err != nil {
		log.WithError(err).Warn("[AntiRaid] Failed to expire raid windows")
		return
	}
	for _, chatID := range expired {
		clearJoinTracking(chatID)
		log.Infof("[AntiRaid] Raid expired for chat %d (auto-expiry)", chatID)
	}
}

func (a *antiRaidStruct) isRaidActive(chatID int64) bool {
	return getRaidState(chatID).Active
}

func (a *antiRaidStruct) enableRaid(chatID int64, durationSeconds int) (bool, error) {
	enabled, err := antiraid.EnableRaid(chatID, durationSeconds)
	if err != nil || !enabled {
		return enabled, err
	}
	clearJoinTracking(chatID)
	return true, nil
}

func (a *antiRaidStruct) disableRaid(chatID int64) (bool, error) {
	disabled, err := antiraid.DisableRaid(chatID)
	if err != nil {
		log.WithError(err).Warnf("[AntiRaid] Failed to disable raid for chat %d", chatID)
		return false, err
	}
	if disabled {
		clearJoinTracking(chatID)
	}
	return disabled, nil
}

func (a *antiRaidStruct) setRaidDuration(chatID int64, durationSeconds int) error {
	return antiraid.SetRaidDuration(chatID, durationSeconds)
}

func banRaidMember(bot *gotgbot.Bot, chat *gotgbot.Chat, userID int64, actionTime int) {
	untilDate, ok := extraction.TemporaryUntilDate(time.Now().Unix(), int64(actionTime))
	if !ok {
		log.Warnf("[AntiRaid] Invalid raid action time %d; refusing permanent ban for user %d in chat %d", actionTime, userID, chat.Id)
		return
	}
	if _, err := chat.BanMember(bot, userID, &gotgbot.BanChatMemberOpts{UntilDate: untilDate}); err != nil {
		log.WithError(err).Warnf("[AntiRaid] Failed to ban user %d in chat %d", userID, chat.Id)
	}
}

func (a *antiRaidStruct) onJoin(bot *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	chat := ctx.EffectiveChat

	if chat == nil {
		return ext.ContinueGroups
	}
	if chat.Type != "group" && chat.Type != "supergroup" {
		return ext.ContinueGroups
	}

	if !chat_status.IsBotAdmin(bot, ctx, chat) {
		return ext.ContinueGroups
	}
	if !chat_status.CanBotRestrict(bot, ctx, chat) {
		log.WithFields(log.Fields{
			"chatId": chat.Id,
		}).Warn("Antiraid action skipped: bot lacks restrict permissions")
		return ext.ContinueGroups
	}

	settings := antiraid.GetAntiRaidSettings(chat.Id)
	isActive := a.isRaidActive(chat.Id)

	for _, member := range msg.NewChatMembers {
		if member.Id == bot.Id {
			continue
		}
		if chat_status.IsApproved(bot, chat.Id, member.Id) {
			continue
		}
		if chat_status.IsUserAdmin(bot, chat.Id, member.Id) {
			continue
		}

		if isActive {
			banRaidMember(bot, chat, member.Id, settings.RaidActionTime)
			continue
		}

		if settings.AutoAntiRaidThreshold <= 0 {
			continue
		}

		count, err := trackJoin(chat.Id, member.Id)
		if err != nil {
			log.WithError(err).Warnf("[AntiRaid] Failed to track join for chat %d", chat.Id)
			continue
		}

		if count >= settings.AutoAntiRaidThreshold {
			enabled, err := a.enableRaid(chat.Id, settings.RaidTime)
			if err != nil {
				log.WithError(err).Warnf("[AntiRaid] Failed to auto-enable raid in chat %d", chat.Id)
				continue
			}
			isActive = true
			if !enabled {
				// Another update crossed the threshold first. Apply the active
				// raid without resetting its expiry or sending a duplicate alert.
				banRaidMember(bot, chat, member.Id, settings.RaidActionTime)
				continue
			}
			log.Infof("[AntiRaid] Auto-triggered raid in chat %d (joins=%d >= threshold=%d)", chat.Id, count, settings.AutoAntiRaidThreshold)

			tr := i18n.English()
			text, _ := tr.GetString("antiraid_auto_triggered", i18n.TranslationParams{"count": strconv.Itoa(count)})
			_, _ = chat.SendMessage(bot, text, formatting.Shtml())

			banRaidMember(bot, chat, member.Id, settings.RaidActionTime)
		}
	}

	return ext.ContinueGroups
}

func (a *antiRaidStruct) antiraid(bot *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveChat
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}

	if !chat_status.RequireGroup(bot, ctx, nil) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_group_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}
	if !chat_status.RequireUserAdmin(bot, ctx, nil, user.Id) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_user_admin_cmd_error", "chat_status_user_admin_button_error", chat_status.WithReplyFallback())
		return ext.EndGroups
	}
	if !chat_status.RequireBotAdmin(bot, ctx, nil) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_bot_not_admin", "", chat_status.WithReply())
		return ext.EndGroups
	}

	tr := i18n.English()
	args := ctx.Args()[1:]

	if len(args) == 0 {
		settings := antiraid.GetAntiRaidSettings(chat.Id)
		isActive := a.isRaidActive(chat.Id)
		st := getRaidState(chat.Id)
		var text string
		if isActive {
			text, _ = tr.GetString("antiraid_active_status", i18n.TranslationParams{
				"raid_time":      formatDuration(settings.RaidTime),
				"action_time":    formatDuration(settings.RaidActionTime),
				"auto_threshold": strconv.Itoa(settings.AutoAntiRaidThreshold),
				"expires_in":     formatDuration(int(int64(st.ExpiresAt) - time.Now().Unix())),
			})
		} else {
			text, _ = tr.GetString("antiraid_inactive_status", i18n.TranslationParams{
				"raid_time":      formatDuration(settings.RaidTime),
				"action_time":    formatDuration(settings.RaidActionTime),
				"auto_threshold": strconv.Itoa(settings.AutoAntiRaidThreshold),
			})
		}

		var kb [][]gotgbot.InlineKeyboardButton
		if isActive {
			disableText, _ := tr.GetString("antiraid_btn_disable")
			kb = append(kb, []gotgbot.InlineKeyboardButton{{
				Text:         disableText,
				CallbackData: encodeCallbackData("antiraid", map[string]string{"a": "off"}),
			}})
		} else {
			enableText, _ := tr.GetString("antiraid_btn_enable")
			kb = append(kb, []gotgbot.InlineKeyboardButton{{
				Text:         enableText,
				CallbackData: encodeCallbackData("antiraid", map[string]string{"a": "on"}),
			}})
		}

		_, _ = msg.Reply(bot, text, &gotgbot.SendMessageOpts{
			ParseMode: formatting.HTML,
			LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
				IsDisabled: true,
			},
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: kb},
		})
		return ext.EndGroups
	}

	arg := strings.ToLower(args[0])
	switch arg {
	case "on":
		settings := antiraid.GetAntiRaidSettings(chat.Id)
		enabled, err := a.enableRaid(chat.Id, settings.RaidTime)
		if err != nil {
			log.WithError(err).Errorf("[AntiRaid] Failed to enable raid in chat %d", chat.Id)
			text, _ := tr.GetString("error_generic")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		if !enabled {
			text, _ := tr.GetString("antiraid_already_active")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ := tr.GetString("antiraid_enabled", i18n.TranslationParams{"duration": formatDuration(settings.RaidTime)})
		_, _ = msg.Reply(bot, text, formatting.Shtml())

	case "off":
		disabled, err := a.disableRaid(chat.Id)
		if err != nil {
			text, _ := tr.GetString("error_generic")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		if !disabled {
			text, _ := tr.GetString("antiraid_not_active")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ := tr.GetString("antiraid_disabled")
		_, _ = msg.Reply(bot, text, formatting.Shtml())

	default:
		dur, ok := parseDuration(arg)
		if !ok {
			text, _ := tr.GetString("antiraid_invalid_duration")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		if err := a.setRaidDuration(chat.Id, dur); err != nil {
			log.WithError(err).Errorf("[AntiRaid] Failed to set raid duration in chat %d", chat.Id)
			text, _ := tr.GetString("error_generic")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ := tr.GetString("antiraid_enabled", i18n.TranslationParams{"duration": formatDuration(dur)})
		_, _ = msg.Reply(bot, text, formatting.Shtml())
	}

	return ext.EndGroups
}

func (a *antiRaidStruct) raidTime(bot *gotgbot.Bot, ctx *ext.Context) error {
	return a.raidTimeSetter(bot, ctx, true)
}

func (a *antiRaidStruct) raidActionTime(bot *gotgbot.Bot, ctx *ext.Context) error {
	return a.raidTimeSetter(bot, ctx, false)
}

//nolint:dupl // Similar patterns by design: raidTime vs raidActionTime commands.
func (a *antiRaidStruct) raidTimeSetter(bot *gotgbot.Bot, ctx *ext.Context, isRaidTime bool) error {
	chat := ctx.EffectiveChat
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}
	if !chat_status.RequireGroup(bot, ctx, nil) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_group_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}
	if !chat_status.RequireUserAdmin(bot, ctx, nil, user.Id) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_user_admin_cmd_error", "chat_status_user_admin_button_error", chat_status.WithReplyFallback())
		return ext.EndGroups
	}
	if !chat_status.RequireBotAdmin(bot, ctx, nil) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_bot_not_admin", "", chat_status.WithReply())
		return ext.EndGroups
	}

	tr := i18n.English()
	args := ctx.Args()[1:]
	if len(args) == 0 {
		text := ""
		if isRaidTime {
			text, _ = tr.GetString("antiraid_raidtime_usage")
		} else {
			text, _ = tr.GetString("antiraid_raidactiontime_usage")
		}
		_, _ = msg.Reply(bot, text, formatting.Shtml())
		return ext.EndGroups
	}

	dur, ok := parseDuration(args[0])
	if ok && !isRaidTime {
		_, ok = extraction.TemporaryUntilDate(0, int64(dur))
	}
	if !ok {
		text, _ := tr.GetString("antiraid_invalid_duration")
		_, _ = msg.Reply(bot, text, formatting.Shtml())
		return ext.EndGroups
	}

	text := ""
	if isRaidTime {
		settings := antiraid.GetAntiRaidSettings(chat.Id)
		if settings.RaidTime == dur {
			text, _ = tr.GetString("antiraid_raidtime_no_change", i18n.TranslationParams{"duration": formatDuration(dur)})
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		err := antiraid.SetRaidTime(chat.Id, dur)
		if err != nil {
			log.WithError(err).Errorf("[AntiRaid] SetRaidTime failed for chat %d", chat.Id)
			text, _ = tr.GetString("common_settings_save_failed")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ = tr.GetString("antiraid_raidtime_set", i18n.TranslationParams{"duration": formatDuration(dur)})
	} else {
		settings := antiraid.GetAntiRaidSettings(chat.Id)
		if settings.RaidActionTime == dur {
			text, _ = tr.GetString("antiraid_raidactiontime_no_change", i18n.TranslationParams{"duration": formatDuration(dur)})
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		err := antiraid.SetRaidActionTime(chat.Id, dur)
		if err != nil {
			log.WithError(err).Errorf("[AntiRaid] SetRaidActionTime failed for chat %d", chat.Id)
			text, _ = tr.GetString("common_settings_save_failed")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ = tr.GetString("antiraid_raidactiontime_set", i18n.TranslationParams{"duration": formatDuration(dur)})
	}
	_, _ = msg.Reply(bot, text, formatting.Shtml())
	return ext.EndGroups
}

func (a *antiRaidStruct) autoAntiRaid(bot *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveChat
	msg := ctx.EffectiveMessage
	user := chat_status.RequireUser(bot, ctx)
	if user == nil {
		return ext.EndGroups
	}
	if !chat_status.RequireGroup(bot, ctx, nil) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_group_only_error", "", chat_status.WithReply())
		return ext.EndGroups
	}
	if !chat_status.RequireUserAdmin(bot, ctx, nil, user.Id) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_user_admin_cmd_error", "chat_status_user_admin_button_error", chat_status.WithReplyFallback())
		return ext.EndGroups
	}
	if !chat_status.RequireBotAdmin(bot, ctx, nil) {
		chat_status.NewPermissionResponder(bot).Respond(ctx, "chat_status_bot_not_admin", "", chat_status.WithReply())
		return ext.EndGroups
	}

	tr := i18n.English()
	args := ctx.Args()[1:]
	if len(args) == 0 {
		settings := antiraid.GetAntiRaidSettings(chat.Id)
		var text string
		if settings.AutoAntiRaidThreshold > 0 {
			text, _ = tr.GetString("antiraid_auto_enabled", i18n.TranslationParams{"threshold": strconv.Itoa(settings.AutoAntiRaidThreshold)})
		} else {
			text, _ = tr.GetString("antiraid_auto_disabled")
		}
		_, _ = msg.Reply(bot, text, formatting.Shtml())
		return ext.EndGroups
	}

	arg := strings.ToLower(args[0])
	if arg == "off" {
		err := antiraid.SetAutoAntiRaidThreshold(chat.Id, 0)
		if err != nil {
			log.WithError(err).Errorf("[AntiRaid] SetAutoAntiRaidThreshold(0) failed for chat %d", chat.Id)
			text, _ := tr.GetString("common_settings_save_failed")
			_, _ = msg.Reply(bot, text, formatting.Shtml())
			return ext.EndGroups
		}
		text, _ := tr.GetString("antiraid_auto_disabled")
		_, _ = msg.Reply(bot, text, formatting.Shtml())
		return ext.EndGroups
	}

	threshold, err := strconv.Atoi(arg)
	if err != nil || threshold <= 0 {
		text, _ := tr.GetString("antiraid_invalid_threshold")
		_, _ = msg.Reply(bot, text, formatting.Shtml())
		return ext.EndGroups
	}
	if err := antiraid.SetAutoAntiRaidThreshold(chat.Id, threshold); err != nil {
		log.WithError(err).Errorf("[AntiRaid] SetAutoAntiRaidThreshold(%d) failed for chat %d", threshold, chat.Id)
		text, _ := tr.GetString("common_settings_save_failed")
		_, _ = msg.Reply(bot, text, formatting.Shtml())
		return ext.EndGroups
	}

	text, _ := tr.GetString("antiraid_auto_enabled", i18n.TranslationParams{"threshold": strconv.Itoa(threshold)})
	_, _ = msg.Reply(bot, text, formatting.Shtml())
	return ext.EndGroups
}

func (a *antiRaidStruct) callbackHandler(bot *gotgbot.Bot, ctx *ext.Context) error {
	query, ok := callbackQueryFromContext(ctx)
	if !ok {
		return ext.ContinueGroups
	}
	if query == nil {
		return ext.ContinueGroups
	}

	action := ""
	data := query.Data
	decoded, ok := decodeCallbackData(data, "antiraid")
	if !ok {
		log.Warnf("[AntiRaid] Ignoring malformed callback data: %s", data)
		return ext.ContinueGroups
	}
	action, _ = decoded.Field("a")

	msg := query.Message
	if msg == nil {
		return ext.ContinueGroups
	}
	chatID := msg.GetChat().Id

	if !chat_status.IsUserAdmin(bot, chatID, query.From.Id) {
		_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{
			Text: "You're not an admin!",
		})
		return ext.EndGroups
	}

	tr := i18n.English()
	switch action {
	case "on":
		settings := antiraid.GetAntiRaidSettings(chatID)
		enabled, err := a.enableRaid(chatID, settings.RaidTime)
		if err != nil {
			log.WithError(err).Errorf("[AntiRaid] Failed to enable raid in chat %d", chatID)
			text, _ := tr.GetString("error_generic")
			_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
			return ext.EndGroups
		}
		if !enabled {
			text, _ := tr.GetString("antiraid_already_active")
			_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
			return ext.EndGroups
		}
		text, _ := tr.GetString("antiraid_enabled", i18n.TranslationParams{"duration": formatDuration(settings.RaidTime)})
		_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		_, _, _ = msg.EditText(bot, tgmd2html.MD2HTMLV2(text), &gotgbot.EditMessageTextOpts{
			ParseMode: formatting.HTML,
		})
	case "off":
		disabled, err := a.disableRaid(chatID)
		if err != nil {
			text, _ := tr.GetString("error_generic")
			_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
			return ext.EndGroups
		}
		if !disabled {
			text, _ := tr.GetString("antiraid_not_active")
			_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
			return ext.EndGroups
		}
		text, _ := tr.GetString("antiraid_disabled")
		_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		_, _, _ = msg.EditText(bot, tgmd2html.MD2HTMLV2(text), &gotgbot.EditMessageTextOpts{
			ParseMode: formatting.HTML,
		})
	default:
		text, _ := tr.GetString("common_callback_invalid_request")
		_, _ = bot.AnswerCallbackQuery(query.Id, &gotgbot.AnswerCallbackQueryOpts{Text: text})
	}

	return ext.EndGroups
}

func parseDuration(input string) (seconds int, ok bool) {
	input = strings.TrimSpace(strings.ToLower(input))
	if len(input) == 0 {
		return 0, false
	}

	var duration int64
	unit := input[len(input)-1]
	// If the last char is a digit, treat the whole string as bare seconds.
	if unit >= '0' && unit <= '9' {
		var err error
		duration, err = strconv.ParseInt(input, 10, 64)
		if err != nil {
			return 0, false
		}
	} else {
		num, err := strconv.ParseInt(input[:len(input)-1], 10, 64)
		if err != nil || num < 0 {
			return 0, false
		}
		var multiplier int64
		switch unit {
		case 's':
			multiplier = 1
		case 'm':
			multiplier = 60
		case 'h':
			multiplier = 60 * 60
		case 'd':
			multiplier = 24 * 60 * 60
		case 'w':
			multiplier = 7 * 24 * 60 * 60
		default:
			return 0, false
		}
		if num > math.MaxInt64/multiplier {
			return 0, false
		}
		duration = num * multiplier
	}

	if duration <= 0 || duration > maxAntiRaidDuration {
		return 0, false
	}
	return int(duration), true
}

func formatDuration(seconds int) string {
	if seconds >= 604800 && seconds%604800 == 0 {
		return fmt.Sprintf("%dw", seconds/604800)
	}
	if seconds >= 86400 && seconds%86400 == 0 {
		return fmt.Sprintf("%dd", seconds/86400)
	}
	if seconds >= 3600 && seconds%3600 == 0 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	if seconds >= 60 && seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func LoadAntiRaid(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[antiRaidModule.moduleName] = true

	dispatcher.AddHandler(handlers.NewCommand("antiraid", antiRaidModule.antiraid))
	dispatcher.AddHandler(handlers.NewCommand("raidtime", antiRaidModule.raidTime))
	dispatcher.AddHandler(handlers.NewCommand("raidactiontime", antiRaidModule.raidActionTime))
	dispatcher.AddHandler(handlers.NewCommand("autoantiraid", antiRaidModule.autoAntiRaid))

	dispatcher.AddHandlerToGroup(
		handlers.NewMessage(
			func(msg *gotgbot.Message) bool {
				return msg.NewChatMembers != nil
			},
			antiRaidModule.onJoin,
		),
		antiRaidModule.handlerGroup,
	)

	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("antiraid"), antiRaidModule.callbackHandler))

	StartAntiRaidExpiryPoller()
}

func init() {
	RegisterLegacyModule("AntiRaid", 230, LoadAntiRaid)
}
