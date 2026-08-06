package modules

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"

	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/cache"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

// function used to get status of bot when it joined a group and send a message to the group
// also send a message to MESSAGE_DUMP telling that it joined a group
// botJoinedGroup handles bot addition to new groups.
// Sends welcome message and ensures the group is a supergroup before staying.
func botJoinedGroup(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveChat

	// don't log if it's a private chat
	if chat.Type == "private" {
		return ext.EndGroups
	}

	// check if group is supergroup or not
	// if not a supergroup, send a message and leave it
	if chat.Type == "group" || chat.Type == "channel" {
		if chat.Type == "group" {
			tr := i18n.English()
			text, _ := tr.GetString("bot_updates_need_supergroup")
			convertInstr, _ := tr.GetString("bot_updates_convert_instruction")
			convertHowto, _ := tr.GetString("bot_updates_convert_howto")
			_, err := b.SendMessage(
				chat.Id,
				fmt.Sprint(
					text,
					convertInstr,
					convertHowto,
					"https://telegra.ph/Convert-group-to-Supergroup-07-29",
				),
				formatting.Shtml(),
			)
			if err != nil {
				log.Error(err)
				return err
			}
		}

		_, err := b.LeaveChat(chat.Id, nil)
		if err != nil {
			log.Error(err)
			return err
		}

		return ext.EndGroups
	}

	msgAdmin := "\n\nMake me admin to use me with my full abilities!"

	// used to check if bot was added as admin or not
	if chat_status.IsBotAdmin(b, ctx, chat) {
		msgAdmin = ""
	}

	// send a message to group itself
	tr := i18n.English()
	thanksText, _ := tr.GetString("bot_updates_thanks_for_adding")
	_, err := b.SendMessage(
		chat.Id,
		fmt.Sprint(thanksText, msgAdmin),
		nil,
	)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.ContinueGroups
}

// adminCacheAutoUpdate automatically refreshes admin cache when admin status changes.
// Reloads admin permissions cache if it's not already available.
func adminCacheAutoUpdate(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveChat
	if chat == nil {
		return ext.ContinueGroups
	}

	// Always invalidate and reload on admin status updates to avoid stale
	// permission decisions from outdated cache entries.
	cache.InvalidateAdminCache(chat.Id)
	cache.LoadAdminCache(b, chat.Id)
	log.Info(fmt.Sprintf("Reloaded admin cache for %d (%s)", chat.Id, chat.Title))

	return ext.ContinueGroups
}

// LoadBotUpdates registers bot event handlers for group management.
// Sets up handlers for bot joins and admin cache updates.
func LoadBotUpdates(dispatcher *ext.Dispatcher) {
	dispatcher.AddHandlerToGroup(
		handlers.NewMyChatMember(
			func(u *gotgbot.ChatMemberUpdated) bool {
				wasMember, isMember := chat_status.ExtractJoinLeftStatusChange(u)
				return !wasMember && isMember
			},
			botJoinedGroup,
		),
		-1, // process before all other handlers
	)

	dispatcher.AddHandler(
		handlers.NewChatMember(
			chat_status.ExtractAdminUpdateStatusChange,
			adminCacheAutoUpdate,
		),
	)

}

func init() {
	RegisterLegacyModule("BotUpdates", -10, LoadBotUpdates)
}
