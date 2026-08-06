package modules

import (
	"fmt"
	"html"
	"slices"
	"strings"
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

var (
	cachedBotUsername   string
	cachedBotUsernameMu sync.RWMutex
)

func getBotUsername(b *gotgbot.Bot) string {
	cachedBotUsernameMu.RLock()
	cached := cachedBotUsername
	cachedBotUsernameMu.RUnlock()
	if cached != "" {
		return cached
	}

	cachedBotUsernameMu.Lock()
	defer cachedBotUsernameMu.Unlock()
	if cachedBotUsername != "" {
		return cachedBotUsername
	}
	if b != nil && b.Username != "" {
		cachedBotUsername = b.Username
		return cachedBotUsername
	}
	if b != nil {
		if me, err := b.GetMe(nil); err == nil && me != nil && me.Username != "" {
			cachedBotUsername = me.Username
		}
	}
	return cachedBotUsername
}

func getStartHelp(tr *i18n.Translator) string {
	text, _ := tr.GetString("help_bot_intro")
	return text
}

func getMainHelp(tr *i18n.Translator, firstName string) string {
	intro, _ := tr.GetString("help_pm_intro", i18n.TranslationParams{"s": firstName})
	usage, _ := tr.GetString("help_all_commands_usage")
	return intro + usage
}

func getStartMarkup(tr *i18n.Translator, _ string) gotgbot.InlineKeyboardMarkup {
	commandsHelpText, _ := tr.GetString("help_button_commands_help")
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{{
		Text:         commandsHelpText,
		CallbackData: encodeCallbackData("helpq", map[string]string{"m": "Help"}),
	}}}}
}

func (moduleStruct) helpButtonHandler(b *gotgbot.Bot, ctx *ext.Context) error {
	query, ok := callbackQueryFromContext(ctx)
	if !ok || query == nil {
		return ext.EndGroups
	}
	if query.Message == nil {
		text, _ := i18n.English().GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}

	module := ""
	if decoded, ok := decodeCallbackData(query.Data, "helpq"); ok {
		module, _ = decoded.Field("m")
	}
	if module == "" {
		text, _ := i18n.English().GetString("common_callback_invalid_request")
		_, _ = query.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text})
		return ext.EndGroups
	}

	var helpText string
	var replyKb gotgbot.InlineKeyboardMarkup
	if slices.Contains([]string{"BackStart", "Help"}, module) {
		tr := i18n.English()
		if module == "Help" {
			helpText = getMainHelp(tr, html.EscapeString(query.From.FirstName))
			replyKb = markup
		} else {
			helpText = getStartHelp(tr)
			replyKb = getStartMarkup(tr, getBotUsername(b))
		}
	} else {
		helpText, replyKb, _ = getHelpTextAndMarkup(ctx, strings.ToLower(module), DefaultHelpRegistry())
	}

	_, _, err := query.Message.EditText(b, helpText, &gotgbot.EditMessageTextOpts{
		ParseMode:   formatting.HTML,
		ReplyMarkup: replyKb,
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
			IsDisabled: true,
		},
	})
	if err != nil {
		return err
	}
	_, err = query.Answer(b, nil)
	if err != nil {
		return err
	}
	return ext.EndGroups
}

func (moduleStruct) start(b *gotgbot.Bot, ctx *ext.Context) error {
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return ext.EndGroups
	}
	msg := ctx.EffectiveMessage
	args := ctx.Args()

	if msg.Chat.Type != "private" {
		text, _ := i18n.English().GetString("help_pm_questions")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			return err
		}
		return ext.EndGroups
	}
	if len(args) == 2 {
		return HandleDeepLink(b, ctx, user, args[1])
	}

	tr := i18n.English()
	startMarkup := getStartMarkup(tr, getBotUsername(b))
	_, err := msg.Reply(b, getStartHelp(tr), &gotgbot.SendMessageOpts{
		ParseMode:   formatting.HTML,
		ReplyMarkup: &startMarkup,
		LinkPreviewOptions: &gotgbot.LinkPreviewOptions{
			IsDisabled: true,
		},
	})
	if err != nil {
		return err
	}
	return ext.EndGroups
}

func (moduleStruct) help(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	args := ctx.Args()

	if msg.Chat.Type == "private" {
		if len(args) == 2 {
			helpText, replyMarkup, parseMode := getHelpTextAndMarkup(ctx, strings.ToLower(args[1]), DefaultHelpRegistry())
			_, err := b.SendMessage(msg.Chat.Id, helpText, &gotgbot.SendMessageOpts{
				ParseMode:   parseMode,
				ReplyMarkup: &replyMarkup,
			})
			if err != nil {
				return err
			}
			return ext.EndGroups
		}

		name := "User"
		if msg.From != nil {
			name = html.EscapeString(msg.From.FirstName)
		}
		_, err := b.SendMessage(msg.Chat.Id, getMainHelp(i18n.English(), name), &gotgbot.SendMessageOpts{
			ParseMode:   formatting.HTML,
			ReplyMarkup: &markup,
		})
		if err != nil {
			return err
		}
		return ext.EndGroups
	}

	buttonText, _ := i18n.English().GetString("help_click_here")
	contactText, _ := i18n.English().GetString("help_contact_pm")
	deepLink := "help_help"
	if len(args) == 2 {
		module := strings.ToLower(args[1])
		if canonical := getModuleNameFromAltName(module, DefaultHelpRegistry()); canonical != "" {
			deepLink = "help_" + module
		}
	}
	_, err := msg.Reply(b, contactText, &gotgbot.SendMessageOpts{
		ParseMode: formatting.HTML,
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{{
			Text: buttonText,
			Url:  fmt.Sprintf("https://t.me/%s?start=%s", getBotUsername(b), deepLink),
		}}}},
	})
	if err != nil {
		return err
	}
	return ext.EndGroups
}

func LoadHelp(dispatcher *ext.Dispatcher) {
	dispatcher.AddHandler(handlers.NewCommand("start", DefaultHelpRegistry().start))
	dispatcher.AddHandler(handlers.NewCommand("help", DefaultHelpRegistry().help))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("helpq"), DefaultHelpRegistry().helpButtonHandler))
	initHelpButtons()
}

func init() {
	RegisterDeepLinkHandler("help_", helpDeepLinkHandler)
}

func helpDeepLinkHandler(b *gotgbot.Bot, ctx *ext.Context, _ *gotgbot.User, arg string) error {
	helpModule := strings.TrimPrefix(arg, "help_")
	if helpModule == "" || strings.Contains(helpModule, "_") {
		text, _ := i18n.English().GetString("helpers_invalid_deep_link")
		_, _ = ctx.EffectiveMessage.Reply(b, text, formatting.Shtml())
		return ext.EndGroups
	}
	if _, err := sendHelpkb(b, ctx, helpModule, DefaultHelpRegistry()); err != nil {
		log.Errorf("[Start]: %v", err)
		return err
	}
	return ext.EndGroups
}
