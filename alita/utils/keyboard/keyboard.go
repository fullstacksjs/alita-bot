// Package keyboard provides utilities for building Telegram inline keyboards.
package keyboard

import (
	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/callbackcodec"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
)

// BuildKeyboard constructs an inline keyboard from a slice of database button objects.
// Handles button grouping based on the SameLine property for proper layout.
func BuildKeyboard(buttons []db.Button) [][]gotgbot.InlineKeyboardButton {
	keyb := make([][]gotgbot.InlineKeyboardButton, 0)
	for _, btn := range buttons {
		if btn.SameLine && len(keyb) > 0 {
			keyb[len(keyb)-1] = append(keyb[len(keyb)-1], gotgbot.InlineKeyboardButton{Text: btn.Name, Url: btn.Url})
		} else {
			k := make([]gotgbot.InlineKeyboardButton, 1)
			k[0] = gotgbot.InlineKeyboardButton{Text: btn.Name, Url: btn.Url}
			keyb = append(keyb, k)
		}
	}
	return keyb
}

// InitButtons creates an inline keyboard markup for the connection menu.
// Shows admin commands button if the user is an admin, otherwise shows only user commands.
func InitButtons(b *gotgbot.Bot, chatId, userId int64) gotgbot.InlineKeyboardMarkup {
	tr := i18n.English()
	adminText, _ := tr.GetString("helpers_admin_commands")
	if adminText == "" {
		adminText = "Admin commands" // fallback
	}
	userText, _ := tr.GetString("helpers_user_commands")
	if userText == "" {
		userText = "User commands" // fallback
	}

	var connButtons [][]gotgbot.InlineKeyboardButton
	if chat_status.IsUserAdmin(b, chatId, userId) {
		connButtons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         adminText,
					CallbackData: callbackcodec.EncodeOrFallback("connbtns", map[string]string{"t": "Admin"}, "connbtns.Admin"),
				},
			},
			{
				{
					Text:         userText,
					CallbackData: callbackcodec.EncodeOrFallback("connbtns", map[string]string{"t": "User"}, "connbtns.User"),
				},
			},
		}
	} else {
		connButtons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         userText,
					CallbackData: callbackcodec.EncodeOrFallback("connbtns", map[string]string{"t": "User"}, "connbtns.User"),
				},
			},
		}
	}
	connKeyboard := gotgbot.InlineKeyboardMarkup{InlineKeyboard: connButtons}
	return connKeyboard
}
