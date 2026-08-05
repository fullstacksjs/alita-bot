package modules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/i18n"
	"github.com/divkix/Alita_Robot/alita/utils/cache"
	"github.com/divkix/Alita_Robot/alita/utils/chat_status"
	"github.com/divkix/Alita_Robot/alita/utils/formatting"
)

var reportsModule = moduleStruct{
	moduleName: "Reports",
}

// report handles the /report command to notify administrators about problematic messages.
func (moduleStruct) report(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveChat
	sender := ctx.EffectiveSender
	if sender == nil || sender.User == nil {
		return ext.EndGroups
	}
	user := sender.User
	msg := ctx.EffectiveMessage

	if msg.ReplyToMessage == nil {
		tr := i18n.English()
		text, _ := tr.GetString("reports_reply_to_report")
		_, _ = msg.Reply(b, text, nil)
		return ext.EndGroups
	}

	// Check if From is nil (channel posts, deleted users)
	if msg.ReplyToMessage.From == nil {
		tr := i18n.English()
		text, _ := tr.GetString("reports_cannot_report_channel")
		_, _ = msg.Reply(b, text, nil)
		return ext.EndGroups
	}

	var (
		replyMsgId int64
		adminArray []int64
		err        error
	)

	if msg.ReplyToMessage.From.Id == user.Id {
		tr := i18n.English()
		text, _ := tr.GetString("reports_cannot_report_self")
		_, _ = msg.Reply(b, text, nil)
		return ext.EndGroups
	}

	if replyMsg := msg.ReplyToMessage; replyMsg != nil {
		replyMsgId = replyMsg.MessageId
	} else {
		replyMsgId = msg.MessageId
	}

	if user.Id == 1087968824 || user.Id == 777000 || user.Id == 136817688 {
		tr := i18n.English()
		text, _ := tr.GetString("reports_expose_yourself")
		_, _ = msg.Reply(b, text, nil)
		return ext.EndGroups
	}
	if msg.ReplyToMessage.From.Id == 1087968824 || msg.ReplyToMessage.From.Id == 777000 || msg.ReplyToMessage.From.Id == 136817688 {
		tr := i18n.English()
		text, _ := tr.GetString("reports_special_account")
		_, _ = msg.Reply(b, text, nil)
		return ext.EndGroups
	}

	if chat_status.IsUserAdmin(b, chat.Id, user.Id) {
		tr := i18n.English()
		text, _ := tr.GetString("reports_admin_report")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	adminsAvail, admins := cache.GetAdminCacheList(chat.Id)
	if !adminsAvail {
		admins = cache.LoadAdminCache(b, chat.Id)
	}

	for i := range admins.UserInfo {
		admin := &admins.UserInfo[i]
		adminArray = append(adminArray, admin.User.Id)
	}

	reportedUser := msg.ReplyToMessage.From

	if reportedUser.Id == b.Id {
		tr := i18n.English()
		text, _ := tr.GetString("reports_why_report_myself")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}
	if slices.Contains(adminArray, reportedUser.Id) {
		tr := i18n.English()
		text, _ := tr.GetString("reports_why_report_admin")
		_, err := msg.Reply(b, text, formatting.Shtml())
		if err != nil {
			log.Error(err)
			return err
		}
		return ext.EndGroups
	}

	tr := i18n.English()
	reportTemplate, _ := tr.GetString("reports_message_template")
	reported := fmt.Sprintf(
		reportTemplate,
		formatting.MentionHtml(user.Id, user.FirstName),
		formatting.MentionHtml(reportedUser.Id, reportedUser.FirstName),
	)
	var sb strings.Builder
	for _, adminUserId := range adminArray {
		sb.WriteString(formatting.MentionHtml(adminUserId, "\u2063"))
	}
	reported += sb.String()

	_, err = msg.Reply(b,
		reported,
		&gotgbot.SendMessageOpts{
			ParseMode: formatting.HTML,
			ReplyParameters: &gotgbot.ReplyParameters{
				MessageId:                replyMsgId,
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

// LoadReports registers the report command handler with the dispatcher.
func LoadReports(dispatcher *ext.Dispatcher) {
	DefaultHelpRegistry().AbleMap[reportsModule.moduleName] = true
	dispatcher.AddHandler(handlers.NewCommand("report", reportsModule.report))
}

func init() {
	RegisterLegacyModule("Reports", 110, LoadReports)
}
