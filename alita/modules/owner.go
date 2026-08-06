package modules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	log "github.com/sirupsen/logrus"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/stats"
	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

var ownerModule = moduleStruct{moduleName: "Owner"}

// isOwner reports whether the update was sent by the configured OWNER_ID.
// It is the only bot-wide authorization check; group-scoped actions keep
// relying on Telegram permissions.
func isOwner(b *gotgbot.Bot, ctx *ext.Context) bool {
	user := chat_status.RequireUser(b, ctx)
	if user == nil {
		return false
	}
	return user.Id == config.AppConfig.OwnerId
}

// chatInfo retrieves and displays detailed information about a specific chat.
// Only accessible by the bot owner. Returns chat name, ID, member count, and invite link.
func (moduleStruct) chatInfo(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isOwner(b, ctx) {
		return ext.ContinueGroups
	}

	msg := ctx.EffectiveMessage
	var replyText string

	args := ctx.Args()

	if len(args) < 2 {
		tr := i18n.English()
		replyText, _ = tr.GetString("owner_specify_chat")
	} else {
		_chatId := args[1]
		chatId, _ := strconv.Atoi(_chatId)
		chat, err := b.GetChat(int64(chatId), nil)
		if err != nil {
			_, _ = msg.Reply(b, err.Error(), nil)
			return ext.EndGroups
		}
		// need to convert chat to group chat to use GetMemberCount
		_chat := chat.ToChat()
		gChat := &_chat
		con, _ := gChat.GetMemberCount(b, nil)
		tr := i18n.English()
		textTemplate, _ := tr.GetString("owner_chat_info")
		replyText = fmt.Sprintf(textTemplate, chat.Title, chat.Id, con, chat.InviteLink)
	}

	_, err := msg.Reply(b, replyText, formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.ContinueGroups
}

// chatList generates and sends a document containing all active chats the bot is in.
// Only accessible by the bot owner. Creates a temporary file with chat IDs and names.
func (moduleStruct) chatList(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isOwner(b, ctx) {
		return ext.ContinueGroups
	}

	msg := ctx.EffectiveMessage
	chat := ctx.EffectiveChat

	tr := i18n.English()
	text, _ := tr.GetString("owner_getting_chat_list")
	rMsg, err := msg.Reply(
		b,
		text,
		nil,
	)
	if err != nil {
		log.Error(err)
		return err
	}

	allChats := chats.GetAllChats()

	var sb strings.Builder
	for chatId, v := range allChats {
		if !v.IsInactive {
			fmt.Fprintf(&sb, "%d: %s\n", chatId, v.ChatName)
		}
	}

	_, err = rMsg.Delete(b, nil)
	if err != nil {
		log.Error(err)
		return err
	}

	_, err = b.SendDocument(
		chat.Id,
		gotgbot.InputFileByReader("chatlist.txt", strings.NewReader(sb.String())),
		&gotgbot.SendDocumentOpts{
			Caption: trS(tr, "owner_chat_list_caption"),
			ReplyParameters: &gotgbot.ReplyParameters{
				MessageId:                msg.MessageId,
				AllowSendingWithoutReply: true,
			},
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.EndGroups
}

// leaveChat makes the bot leave a specified chat.
// Only accessible by the bot owner. Requires chat ID as argument.
func (moduleStruct) leaveChat(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isOwner(b, ctx) {
		return ext.ContinueGroups
	}

	msg := ctx.EffectiveMessage
	args := ctx.Args()

	if len(args) < 2 {
		tr := i18n.English()
		replyText, _ := tr.GetString("owner_specify_chat")
		_, err := msg.Reply(b, replyText, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.ContinueGroups
	}

	chatId, _ := strconv.ParseInt(args[1], 10, 64)

	_, err := b.LeaveChat(chatId, nil)
	if err != nil {
		log.Error(err)
		return err
	}

	tr := i18n.English()
	text, _ := tr.GetString("owner_left_chat")
	_, err = msg.Reply(b, text, formatting.Shtml())
	if err != nil {
		log.Error(err)
		return err
	}

	return ext.ContinueGroups
}

// getStats retrieves and displays bot statistics including user counts, chat counts, and other metrics.
// Only accessible by the bot owner. Shows comprehensive bot usage statistics.
func (moduleStruct) getStats(b *gotgbot.Bot, ctx *ext.Context) error {
	if !isOwner(b, ctx) {
		return ext.ContinueGroups
	}

	msg := ctx.EffectiveMessage
	tr := i18n.English()
	text, _ := tr.GetString("owner_fetching_stats")
	edits, err := msg.Reply(
		b,
		text,
		&gotgbot.SendMessageOpts{
			ParseMode: formatting.HTML,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}

	_, _, err = edits.EditText(
		b,
		stats.LoadAllStats(),
		&gotgbot.EditMessageTextOpts{
			ParseMode: formatting.HTML,
		},
	)
	if err != nil {
		log.Error(err)
		return err
	}
	return ext.ContinueGroups
}

// LoadOwner registers the owner-only operational command handlers with the dispatcher.
// Authorization is bot-wide and relies solely on OWNER_ID.
func LoadOwner(dispatcher *ext.Dispatcher) {
	dispatcher.AddHandler(handlers.NewCommand("stats", ownerModule.getStats))
	dispatcher.AddHandler(handlers.NewCommand("chatinfo", ownerModule.chatInfo))
	dispatcher.AddHandler(handlers.NewCommand("chatlist", ownerModule.chatList))
	dispatcher.AddHandler(handlers.NewCommand("leavechat", ownerModule.leaveChat))
}

func init() {
	RegisterLegacyModule("Owner", 120, LoadOwner)
}
