package modules

import (
	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
)

// canUserConnectToChat authorizes connections solely by current Telegram group administration.
func canUserConnectToChat(b *gotgbot.Bot, chatID, userID int64) (bool, string) {
	if chat_status.IsUserAdmin(b, chatID, userID) {
		return true, ""
	}

	return false, "connections_is_user_connected_user_not_admin"
}
