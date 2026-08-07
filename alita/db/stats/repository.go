package stats

import (
	"fmt"
	"runtime"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db/antiflood"
	"github.com/divkix/Alita_Robot/alita/db/blacklists"
	"github.com/divkix/Alita_Robot/alita/db/channels"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/connections"
	"github.com/divkix/Alita_Robot/alita/db/filters"
	"github.com/divkix/Alita_Robot/alita/db/greetings"
	"github.com/divkix/Alita_Robot/alita/db/notes"
	"github.com/divkix/Alita_Robot/alita/db/user"
	"github.com/dustin/go-humanize"
)

// LoadAllStats generates a comprehensive statistics report for the bot.
// Includes user counts, chat statistics, feature usage, activity metrics, and system information.
func LoadAllStats() string {
	totalUsers := user.LoadUsersStats()
	activeChats, inactiveChats := chats.LoadChatStats()
	dag, wag, mag := chats.LoadActivityStats()
	dau, wau, mau := user.LoadUserActivityStats()
	antiCount := antiflood.LoadAntifloodStats()
	blacklistTriggers, blacklistChats := blacklists.LoadBlacklistsStats()
	connectedUsers, _ := connections.LoadConnectionStats()
	filtersNum, filtersChats := filters.LoadFilterStats()
	enabledWelcome, cleanServiceEnabled, cleanWelcomeEnabled := greetings.LoadGreetingsStats()
	notesNum, notesChats := notes.LoadNotesStats()
	numChannels := channels.LoadChannelStats()

	// Get webhook status information
	var deploymentMode, webhookInfo string
	if config.AppConfig.UseWebhooks {
		deploymentMode = "🌐 Webhook"
		if config.AppConfig.WebhookDomain != "" {
			webhookInfo = fmt.Sprintf("\n    <b>Webhook URL:</b> %s/webhook/***", config.AppConfig.WebhookDomain)
		} else {
			webhookInfo = "\n    <b>Webhook URL:</b> Not configured"
		}
	} else {
		deploymentMode = "🔄 Polling"
		webhookInfo = "\n    <b>Update Method:</b> Long polling"
	}

	result := "<u>Alita's Stats:</u>" +
		fmt.Sprintf("\n\n<b>Deployment Mode:</b> %s%s", deploymentMode, webhookInfo) +
		fmt.Sprintf("\n<b>Go Version:</b> %s", runtime.Version()) +
		fmt.Sprintf("\n<b>Goroutines:</b> %s", humanize.Comma(int64(runtime.NumGoroutine()))) +
		fmt.Sprintf("\n<b>Antiflood:</b> enabled in %s chats", humanize.Comma(antiCount)) +
		fmt.Sprintf(
			"\n<b>Users:</b> %s users found in %s active Chats (%s Inactive, %s Total)",
			humanize.Comma(totalUsers),
			humanize.Comma(int64(activeChats)),
			humanize.Comma(int64(inactiveChats)),
			humanize.Comma(int64(activeChats+inactiveChats)),
		) +
		"\n<b>Group Activity Metrics:</b>" +
		fmt.Sprintf("\n    <b>Daily Active Groups (DAG):</b> %s", humanize.Comma(dag)) +
		fmt.Sprintf("\n    <b>Weekly Active Groups (WAG):</b> %s", humanize.Comma(wag)) +
		fmt.Sprintf("\n    <b>Monthly Active Groups (MAG):</b> %s", humanize.Comma(mag)) +
		"\n<b>User Activity Metrics:</b>" +
		fmt.Sprintf("\n    <b>Daily Active Users (DAU):</b> %s", humanize.Comma(dau)) +
		fmt.Sprintf("\n    <b>Weekly Active Users (WAU):</b> %s", humanize.Comma(wau)) +
		fmt.Sprintf("\n    <b>Monthly Active Users (MAU):</b> %s", humanize.Comma(mau)) +
		fmt.Sprintf(
			"\n<b>Blacklists:</b> %s triggers in %s chats",
			humanize.Comma(blacklistTriggers),
			humanize.Comma(blacklistChats),
		) +
		"\n<b>Connections:</b>" +
		fmt.Sprintf("\n    %s users connected to chats", humanize.Comma(connectedUsers)) +
		fmt.Sprintf(
			"\n<b>Filters:</b> %s filters saved in %s chats",
			humanize.Comma(filtersNum),
			humanize.Comma(filtersChats),
		) +
		"\n<b>Greetings:</b>" +
		fmt.Sprintf("\n    <b>Welcome Enabled:</b> %s", humanize.Comma(enabledWelcome)) +
		fmt.Sprintf("\n    <b>CleanService:</b> %s", humanize.Comma(cleanServiceEnabled)) +
		fmt.Sprintf("\n    <b>CleanWelcome:</b> %s", humanize.Comma(cleanWelcomeEnabled)) +
		fmt.Sprintf(
			"\n<b>Notes:</b> %s notes saved in %s chats",
			humanize.Comma(notesNum),
			humanize.Comma(notesChats),
		) +
		fmt.Sprintf("\n<b>Channels Stored</b>: %s", humanize.Comma(numChannels))

	return result
}
