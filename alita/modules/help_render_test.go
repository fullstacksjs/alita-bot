//go:build testtools

package modules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/divkix/Alita_Robot/alita/i18n"
)

type helpFakeBotClient struct {
	response json.RawMessage
	err      error
}

func (f helpFakeBotClient) RequestWithContext(context.Context, string, string, map[string]any, *gotgbot.RequestOpts) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f helpFakeBotClient) GetAPIURL(*gotgbot.RequestOpts) string {
	return "https://api.telegram.org"
}

func (f helpFakeBotClient) FileURL(token string, path string, _ *gotgbot.RequestOpts) string {
	return "https://api.telegram.org/file/bot" + token + "/" + path
}

func helpTestTranslator(t *testing.T) *i18n.Translator {
	t.Helper()

	tr, err := i18n.NewTestTranslator(`
help_bot_intro: "Intro."
help_pm_intro: "Hi %s. "
help_all_commands_usage: "Use commands."
help_button_commands_help: "Commands"
`)
	if err != nil {
		t.Fatalf("NewTestTranslator() error = %v", err)
	}
	return tr
}

func TestHelpTextRendering(t *testing.T) {
	t.Parallel()

	tr := helpTestTranslator(t)
	if got := getStartHelp(tr); got != "Intro." {
		t.Fatalf("getStartHelp() = %q", got)
	}
	if got := getMainHelp(tr, "Div"); got != "Hi Div. Use commands." {
		t.Fatalf("getMainHelp() = %q", got)
	}
}

func TestHelpKeyboardsUseCallbackCodecAndBotUsername(t *testing.T) {
	t.Parallel()

	tr := helpTestTranslator(t)
	startKb := getStartMarkup(tr, "AlitaRobot")
	if len(startKb.InlineKeyboard) != 1 {
		t.Fatalf("getStartMarkup() rows = %d, want 1", len(startKb.InlineKeyboard))
	}
	if !strings.HasPrefix(startKb.InlineKeyboard[0][0].CallbackData, "helpq|v1|") {
		t.Fatalf("commands callback = %q, want encoded help callback", startKb.InlineKeyboard[0][0].CallbackData)
	}
}

func resetCachedBotUsername() {
	cachedBotUsernameMu.Lock()
	cachedBotUsername = ""
	cachedBotUsernameMu.Unlock()
}

func TestGetBotUsernameCachesStructAndGetMeFallbacks(t *testing.T) {
	resetCachedBotUsername()
	t.Cleanup(resetCachedBotUsername)

	if got := getBotUsername(&gotgbot.Bot{User: gotgbot.User{Username: "StructBot"}}); got != "StructBot" {
		t.Fatalf("getBotUsername(struct) = %q", got)
	}
	if got := getBotUsername(nil); got != "StructBot" {
		t.Fatalf("getBotUsername(cached) = %q", got)
	}

	resetCachedBotUsername()
	bot := &gotgbot.Bot{
		Token: "123:test",
		BotClient: helpFakeBotClient{response: json.RawMessage(
			`{"id":123,"is_bot":true,"first_name":"Alita","username":"GetMeBot"}`,
		)},
	}
	if got := getBotUsername(bot); got != "GetMeBot" {
		t.Fatalf("getBotUsername(getMe) = %q", got)
	}

	resetCachedBotUsername()
	if got := getBotUsername(nil); got != "" {
		t.Fatalf("getBotUsername(nil) = %q, want empty", got)
	}
}
