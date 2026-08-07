package cache

import (
	"github.com/PaulSonOfLars/gotgbot/v2"
)

type AdminCache struct {
	ChatId   int64
	UserInfo []gotgbot.MergedChatMember
	UserMap  map[int64]gotgbot.MergedChatMember // O(1) lookup map
	Cached   bool
}
