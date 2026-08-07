package backup

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/db/user"
)

func TestAllDomainsRoundTripEveryMeaningfulField(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	warnUserID := srcChat + 2
	staleWarnUserID := srcChat + 3
	connectedUserID := srcChat + 4
	approvedUserID := srcChat + 5
	require.NoError(t, chats.EnsureChatInDb(srcChat, "backup_source"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "backup_destination"))
	require.NoError(t, user.EnsureUserInDb(warnUserID, "", ""))
	require.NoError(t, user.EnsureUserInDb(staleWarnUserID, "", ""))
	require.NoError(t, user.EnsureUserInDb(connectedUserID, "", ""))
	require.NoError(t, user.EnsureUserInDb(approvedUserID, "", ""))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
		require.NoError(t, db.DB.Where("user_id IN ?", []int64{warnUserID, staleWarnUserID, connectedUserID, approvedUserID}).Delete(&models.User{}).Error)
	})

	buttons := models.ButtonArray{{Name: "docs", Url: "https://example.com", SameLine: true}}
	require.NoError(t, db.DB.Create(&models.AntifloodSettings{
		ChatId:                 srcChat,
		Limit:                  9,
		Action:                 "tmute",
		DeleteAntifloodMessage: true,
	}).Error)
	require.NoError(t, db.DB.Create(&models.AntiRaidSettings{
		ChatID:                srcChat,
		RaidTime:              7777,
		RaidActionTime:        8888,
		AutoAntiRaidThreshold: 12,
	}).Error)
	require.NoError(t, db.DB.Create(&models.ApprovedUsers{
		ChatID: srcChat, UserID: approvedUserID, ApprovedBy: 202, Reason: "trusted",
	}).Error)
	require.NoError(t, db.DB.Create(&models.BlacklistSettings{
		ChatId: srcChat, Word: "scam", Action: "tban", Reason: "custom reason",
	}).Error)
	require.NoError(t, db.DB.Create(&models.ConnectionSettings{
		UserId: connectedUserID, ChatId: srcChat, Connected: true,
	}).Error)
	require.NoError(t, db.DB.Create(&models.ChatFilters{
		ChatId: srcChat, KeyWord: "hello", FilterReply: "world", MsgType: 2,
		FileID: "filter-file", NoNotif: true, Buttons: buttons,
	}).Error)
	require.NoError(t, db.DB.Create(&models.GreetingSettings{
		ChatID:             srcChat,
		ShouldCleanService: true,
		WelcomeSettings: &models.WelcomeSettings{
			CleanWelcome: true, LastMsgId: 111, ShouldWelcome: true,
			WelcomeText: "welcome", FileID: "welcome-file", WelcomeType: 2, Button: buttons,
		},
	}).Error)
	require.NoError(t, db.DB.Model(&models.GreetingSettings{}).
		Where("chat_id = ?", srcChat).
		Update("welcome_enabled", false).Error)
	require.NoError(t, db.DB.Create(&models.NotesSettings{ChatId: srcChat, Private: true}).Error)
	require.NoError(t, db.DB.Create(&models.Notes{
		ChatId: srcChat, NoteName: "policy", NoteContent: "read it", FileID: "note-file",
		MsgType: 4, Buttons: buttons, AdminOnly: true, PrivateOnly: true,
		GroupOnly: false, WebPreview: true, IsProtected: true, NoNotif: true,
	}).Error)
	require.NoError(t, db.DB.Model(&models.Notes{}).
		Where("chat_id = ?", srcChat).
		Update("web_preview", false).Error)
	require.NoError(t, db.DB.Create(&models.Reactions{
		ChatID: srcChat, Keyword: "nice", Emoji: "🔥",
	}).Error)
	require.NoError(t, db.DB.Create(&models.WarnSettings{
		ChatId: srcChat, WarnLimit: 7,
	}).Error)
	require.NoError(t, db.DB.Create(&models.WarnEvent{
		UserId: warnUserID, ChatId: srcChat, Reason: "one",
	}).Error)
	require.NoError(t, db.DB.Create(&models.WarnEvent{
		UserId: warnUserID, ChatId: srcChat, Reason: "two",
	}).Error)

	// Existing destination data must be replaced, not merged.
	require.NoError(t, db.DB.Create(&models.Reactions{
		ChatID: dstChat, Keyword: "stale", Emoji: "❌",
	}).Error)
	require.NoError(t, db.DB.Create(&models.ChatFilters{
		ChatId: dstChat, KeyWord: "stale", FilterReply: "stale",
	}).Error)
	require.NoError(t, db.DB.Create(&models.WarnEvent{
		UserId: staleWarnUserID, ChatId: dstChat, Reason: "stale",
	}).Error)

	exported, err := ExportChatData(srcChat, "source", 42, nil)
	require.NoError(t, err)
	require.Len(t, exported.Data, len(AllDomains()))
	assert.Equal(t, AllDomains(), exported.Domains)
	assert.Equal(t, BackupFormatVersion, exported.Version)

	raw, err := exported.ToJSON()
	require.NoError(t, err)
	decoded, err := BackupFormatFromJSON(raw)
	require.NoError(t, err)
	require.NoError(t, ImportChatData(dstChat, decoded, nil))

	antifloodData, err := exportAntifloodData(db.DB, dstChat)
	require.NoError(t, err)
	require.NotNil(t, antifloodData.Settings)
	assert.Equal(t, 9, antifloodData.Settings.Limit)
	assert.Equal(t, "tmute", antifloodData.Settings.Action)
	assert.True(t, antifloodData.Settings.DeleteAntifloodMessage)

	antiraidData, err := exportAntiraidData(db.DB, dstChat)
	require.NoError(t, err)
	require.NotNil(t, antiraidData.Settings)
	assert.Equal(t, 7777, antiraidData.Settings.RaidTime)
	assert.Equal(t, 8888, antiraidData.Settings.RaidActionTime)
	assert.Equal(t, 12, antiraidData.Settings.AutoAntiRaidThreshold)

	approvalsData, err := exportApprovalsData(db.DB, dstChat)
	require.NoError(t, err)
	require.Len(t, approvalsData.ApprovedUsers, 1)
	assert.Equal(t, approvedUserID, approvalsData.ApprovedUsers[0].UserID)
	assert.Equal(t, int64(202), approvalsData.ApprovedUsers[0].ApprovedBy)
	assert.Equal(t, "trusted", approvalsData.ApprovedUsers[0].Reason)

	blacklistsData, err := exportBlacklistsData(db.DB, dstChat)
	require.NoError(t, err)
	require.Len(t, blacklistsData.Entries, 1)
	assert.Equal(t, "scam", blacklistsData.Entries[0].Word)
	assert.Equal(t, "tban", blacklistsData.Entries[0].Action)
	assert.Equal(t, "custom reason", blacklistsData.Entries[0].Reason)

	connectionsData, err := exportConnectionsData(db.DB, dstChat)
	require.NoError(t, err)
	require.Len(t, connectionsData.Connections, 1)
	assert.Equal(t, connectedUserID, connectionsData.Connections[0].UserId)
	assert.True(t, connectionsData.Connections[0].Connected)

	filtersData, err := exportFiltersData(db.DB, dstChat)
	require.NoError(t, err)
	require.Len(t, filtersData.Filters, 1)
	assert.Equal(t, "hello", filtersData.Filters[0].KeyWord)
	assert.Equal(t, "world", filtersData.Filters[0].FilterReply)
	assert.Equal(t, 2, filtersData.Filters[0].MsgType)
	assert.Equal(t, "filter-file", filtersData.Filters[0].FileID)
	assert.True(t, filtersData.Filters[0].NoNotif)
	assert.Equal(t, buttons, filtersData.Filters[0].Buttons)

	welcomeData, err := exportWelcomeData(db.DB, dstChat)
	require.NoError(t, err)
	require.NotNil(t, welcomeData.Settings)
	assert.True(t, welcomeData.Settings.ShouldCleanService)
	require.NotNil(t, welcomeData.Settings.WelcomeSettings)
	assert.True(t, welcomeData.Settings.WelcomeSettings.CleanWelcome)
	assert.Equal(t, int64(111), welcomeData.Settings.WelcomeSettings.LastMsgId)
	assert.False(t, welcomeData.Settings.WelcomeSettings.ShouldWelcome)
	assert.Equal(t, "welcome", welcomeData.Settings.WelcomeSettings.WelcomeText)
	assert.Equal(t, "welcome-file", welcomeData.Settings.WelcomeSettings.FileID)
	assert.Equal(t, 2, welcomeData.Settings.WelcomeSettings.WelcomeType)
	assert.Equal(t, buttons, welcomeData.Settings.WelcomeSettings.Button)

	notesData, err := exportNotesData(db.DB, dstChat)
	require.NoError(t, err)
	require.NotNil(t, notesData.Settings)
	assert.True(t, notesData.Settings.Private)
	require.Len(t, notesData.Notes, 1)
	note := notesData.Notes[0]
	assert.Equal(t, "policy", note.NoteName)
	assert.Equal(t, "read it", note.NoteContent)
	assert.Equal(t, "note-file", note.FileID)
	assert.Equal(t, 4, note.MsgType)
	assert.Equal(t, buttons, note.Buttons)
	assert.True(t, note.AdminOnly)
	assert.True(t, note.PrivateOnly)
	assert.False(t, note.GroupOnly)
	assert.False(t, note.WebPreview)
	assert.True(t, note.IsProtected)
	assert.True(t, note.NoNotif)

	reactionsData, err := exportReactionsData(db.DB, dstChat)
	require.NoError(t, err)
	require.Len(t, reactionsData.Reactions, 1)
	assert.Equal(t, "nice", reactionsData.Reactions[0].Keyword)
	assert.Equal(t, "🔥", reactionsData.Reactions[0].Emoji)

	warningsData, err := exportWarningsData(db.DB, dstChat)
	require.NoError(t, err)
	require.NotNil(t, warningsData.Settings)
	assert.Equal(t, 7, warningsData.Settings.WarnLimit)
	require.Len(t, warningsData.Warns, 2)
	assert.Equal(t, warnUserID, warningsData.Warns[0].UserId)
	assert.Equal(t, "one", warningsData.Warns[0].Reason)
	assert.Equal(t, "two", warningsData.Warns[1].Reason)
}

func TestExportChatDataReturnsDatabaseErrors(t *testing.T) {
	original := db.DB
	db.DB = nil
	t.Cleanup(func() { db.DB = original })

	_, err := ExportChatData(1, "chat", 2, []string{DomainFilters})
	require.ErrorContains(t, err, "database not initialized")
}

func TestExportChatDataRejectsRemovedDomains(t *testing.T) {
	for _, domain := range []string{"admin", "disabling", "pins", "rules", "greetings", "warns", "bogus"} {
		_, err := ExportChatData(1, "chat", 2, []string{domain})
		require.ErrorContains(t, err, "unsupported domain: "+domain)
	}
}

func TestClearChatDataRejectsRemovedDomains(t *testing.T) {
	for _, domain := range []string{"admin", "disabling", "pins", "rules", "bogus"} {
		require.ErrorContains(t, ClearChatData(1, []string{domain}), "unsupported domain: "+domain)
	}
}

func TestImportChatDataRejectsHistoricalVersions(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	raw := fmt.Sprintf(`{
		"version":"1.1",
		"bot_name":"AlitaRobot",
		"chat_id":%d,
		"modules":["notes"],
		"data":{"notes":{"notes":[{"note_name":"new"}]}}
	}`, chatID)
	legacy, err := BackupFormatFromJSON([]byte(raw))
	require.NoError(t, err)

	require.ErrorContains(t, ImportChatData(chatID, legacy, nil), `unsupported backup version "1.1"`)

	var count int64
	require.NoError(t, db.DB.Model(&models.Notes{}).Where("chat_id = ?", chatID).Count(&count).Error)
	assert.Zero(t, count, "a rejected import must not write any row")
}

func TestImportChatDataRejectsRemovedDomainsBeforeMutation(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "backup_removed_domain"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })
	require.NoError(t, db.DB.Create(&models.Notes{
		ChatId: chatID, NoteName: "keep", NoteContent: "keep",
	}).Error)

	bkp := NewBackupFormat(chatID, "chat", 1, []string{DomainNotes, "rules"})
	bkp.Data[DomainNotes] = map[string]interface{}{
		"notes": []interface{}{map[string]interface{}{"note_name": "replacement"}},
	}
	bkp.Data["rules"] = map[string]interface{}{"settings": map[string]interface{}{"rules": "nope"}}

	require.ErrorContains(t, ImportChatData(chatID, bkp, nil), "unsupported domain: rules")

	notes, err := findChatRows[models.Notes](db.DB, chatID)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, "keep", notes[0].NoteName, "rejection must happen before any domain is written")
}

func TestImportChatDataRollsBackEarlierDomains(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "backup_atomicity"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })
	require.NoError(t, db.DB.Create(&models.Notes{
		ChatId: chatID, NoteName: "original", NoteContent: "original",
	}).Error)

	backup := NewBackupFormat(chatID, "chat", 1, []string{DomainNotes, DomainAntiflood})
	backup.Data[DomainNotes] = map[string]interface{}{
		"notes": []interface{}{map[string]interface{}{"note_name": "replacement"}},
	}
	backup.Data[DomainAntiflood] = "invalid_payload_type"

	require.Error(t, ImportChatData(chatID, backup, nil))
	notes, err := findChatRows[models.Notes](db.DB, chatID)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, "original", notes[0].NoteName)
}

func TestClearChatDataRollsBackEarlierDomains(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "backup_reset_atomicity"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })
	require.NoError(t, db.DB.Create(&models.Notes{
		ChatId: chatID, NoteName: "keep", NoteContent: "keep",
	}).Error)

	// The unknown domain aborts the transaction, so the notes cleared first must
	// come back.
	require.Error(t, ClearChatData(chatID, []string{DomainNotes, "rules"}))
	notes, err := findChatRows[models.Notes](db.DB, chatID)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, "keep", notes[0].NoteName)
}

func TestImportWarningsCreatesMissingParents(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	userID := chatID + 1
	t.Cleanup(func() {
		cleanupBackupChat(t, chatID)
		require.NoError(t, db.DB.Where("user_id = ?", userID).Delete(&models.User{}).Error)
	})

	bkp := NewBackupFormat(chatID, "fresh", 1, []string{DomainWarnings})
	bkp.Data[DomainWarnings] = map[string]interface{}{
		"settings": map[string]interface{}{"warn_limit": 3},
		"warns": []interface{}{
			map[string]interface{}{"user_id": userID, "reason": "reason"},
		},
	}
	require.NoError(t, ImportChatData(chatID, bkp, nil))

	require.NoError(t, db.DB.Where("chat_id = ?", chatID).Take(&models.Chat{}).Error)
	require.NoError(t, db.DB.Where("user_id = ?", userID).Take(&models.User{}).Error)
	require.NoError(t, db.DB.Where("chat_id = ? AND user_id = ?", chatID, userID).Take(&models.WarnEvent{}).Error)
}

func TestImportConnectionsMovesAnExistingConnection(t *testing.T) {
	skipIfNoDb(t)

	otherChat := time.Now().UnixNano()
	chatID := otherChat + 1
	userID := otherChat + 2
	require.NoError(t, chats.EnsureChatInDb(otherChat, "backup_conn_other"))
	require.NoError(t, chats.EnsureChatInDb(chatID, "backup_conn_target"))
	require.NoError(t, user.EnsureUserInDb(userID, "", ""))
	t.Cleanup(func() {
		cleanupBackupChat(t, otherChat)
		cleanupBackupChat(t, chatID)
		require.NoError(t, db.DB.Where("user_id = ?", userID).Delete(&models.User{}).Error)
	})
	require.NoError(t, db.DB.Create(&models.ConnectionSettings{
		UserId: userID, ChatId: otherChat, Connected: true,
	}).Error)

	bkp := NewBackupFormat(chatID, "chat", 1, []string{DomainConnections})
	bkp.Data[DomainConnections] = map[string]interface{}{
		"connections": []interface{}{
			map[string]interface{}{"user_id": userID, "connected": true},
		},
	}
	require.NoError(t, ImportChatData(chatID, bkp, nil))

	rows, err := findChatRows[models.ConnectionSettings](db.DB, chatID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, userID, rows[0].UserId)
	assert.True(t, rows[0].Connected)

	stale, err := findChatRows[models.ConnectionSettings](db.DB, otherChat)
	require.NoError(t, err)
	assert.Empty(t, stale)
}
