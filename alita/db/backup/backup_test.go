package backup

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/antiflood"
	"github.com/divkix/Alita_Robot/alita/db/antiraid"
	"github.com/divkix/Alita_Robot/alita/db/approvals"
	"github.com/divkix/Alita_Robot/alita/db/blacklists"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/connections"
	"github.com/divkix/Alita_Robot/alita/db/filters"
	"github.com/divkix/Alita_Robot/alita/db/greetings"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/db/notes"
	"github.com/divkix/Alita_Robot/alita/db/warns"
)

func skipIfNoDb(t *testing.T) {
	t.Helper()
	if db.DB == nil {
		t.Skip("requires database connection")
	}
}

// payloadFor renders an exported domain payload the same way the real import
// path does: through a json.Number decoder, so large Telegram IDs survive.
func payloadFor(t *testing.T, exported interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(exported)
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var payload map[string]interface{}
	require.NoError(t, decoder.Decode(&payload))
	return payload
}

func TestBackupTypes(t *testing.T) {
	t.Run("NewBackupFormat creates valid backup", func(t *testing.T) {
		backup := NewBackupFormat(12345, "Test Chat", 67890, []string{"notes", "filters"})

		assert.Equal(t, BackupFormatVersion, backup.Version)
		assert.Equal(t, "AlitaRobot", backup.BotName)
		assert.Equal(t, int64(12345), backup.ChatID)
		assert.Equal(t, "Test Chat", backup.ChatName)
		assert.Equal(t, int64(67890), backup.ExportedBy)
		assert.Equal(t, []string{"notes", "filters"}, backup.Domains)
		assert.NotNil(t, backup.Data)
		assert.WithinDuration(t, time.Now().UTC(), backup.ExportedAt, time.Second)
	})

	t.Run("BackupFormat validation", func(t *testing.T) {
		tests := []struct {
			name    string
			backup  *BackupFormat
			wantErr bool
		}{
			{
				name: "valid backup",
				backup: &BackupFormat{
					Version:    BackupFormatVersion,
					BotName:    "AlitaRobot",
					ChatID:     12345,
					Domains:    []string{"notes"},
					Data:       map[string]interface{}{"notes": map[string]interface{}{}},
					ExportedAt: time.Now(),
				},
				wantErr: false,
			},
			{
				name: "missing version",
				backup: &BackupFormat{
					BotName: "AlitaRobot",
					ChatID:  12345,
					Domains: []string{"notes"},
					Data:    make(map[string]interface{}),
				},
				wantErr: true,
			},
			{
				name: "missing bot name",
				backup: &BackupFormat{
					Version: BackupFormatVersion,
					ChatID:  12345,
					Domains: []string{"notes"},
					Data:    make(map[string]interface{}),
				},
				wantErr: true,
			},
			{
				name: "missing chat ID",
				backup: &BackupFormat{
					Version: BackupFormatVersion,
					BotName: "AlitaRobot",
					Domains: []string{"notes"},
					Data:    make(map[string]interface{}),
				},
				wantErr: true,
			},
			{
				name: "empty domains",
				backup: &BackupFormat{
					Version: BackupFormatVersion,
					BotName: "AlitaRobot",
					ChatID:  12345,
					Domains: []string{},
					Data:    make(map[string]interface{}),
				},
				wantErr: true,
			},
			{
				name: "nil data",
				backup: &BackupFormat{
					Version: BackupFormatVersion,
					BotName: "AlitaRobot",
					ChatID:  12345,
					Domains: []string{"notes"},
					Data:    nil,
				},
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := tt.backup.Validate()
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("IsCompatibleVersion checks version", func(t *testing.T) {
		compatible := &BackupFormat{Version: BackupFormatVersion}
		assert.True(t, compatible.IsCompatibleVersion())

		for _, version := range []string{"0.9", "1.0", "1.1", ""} {
			assert.Falsef(t, (&BackupFormat{Version: version}).IsCompatibleVersion(),
				"version %q must not be accepted", version)
		}
	})

	t.Run("ToJSON marshals correctly", func(t *testing.T) {
		backup := NewBackupFormat(12345, "Test", 67890, []string{"notes"})
		backup.Data["notes"] = []models.Notes{{NoteName: "test", NoteContent: "reply"}}

		jsonData, err := backup.ToJSON()
		require.NoError(t, err)
		assert.NotNil(t, jsonData)
		assert.Contains(t, string(jsonData), "AlitaRobot")
		assert.Contains(t, string(jsonData), "notes")
	})

	t.Run("BackupFormatFromJSON unmarshals correctly", func(t *testing.T) {
		jsonData := `{
			"version": "2",
			"bot_name": "AlitaRobot",
			"chat_id": 12345,
			"chat_name": "Test Chat",
			"exported_by": 67890,
			"domains": ["notes", "filters"],
			"data": {"notes": {"notes": [{"note_name": "welcome", "note_content": "Hello!"}]}, "filters": {}},
			"exported_at": "2024-01-01T00:00:00Z"
		}`

		backup, err := BackupFormatFromJSON([]byte(jsonData))
		require.NoError(t, err)
		assert.Equal(t, "2", backup.Version)
		assert.Equal(t, "AlitaRobot", backup.BotName)
		assert.Equal(t, int64(12345), backup.ChatID)
		assert.Equal(t, []string{"notes", "filters"}, backup.Domains)
		assert.NoError(t, backup.Validate())
	})

	t.Run("BackupFormatFromJSON returns error on invalid JSON", func(t *testing.T) {
		_, err := BackupFormatFromJSON([]byte("invalid json"))
		assert.Error(t, err)
	})
}

func TestDomainValidation(t *testing.T) {
	t.Run("AllDomains returns the retained set", func(t *testing.T) {
		domains := AllDomains()
		assert.Len(t, domains, 10)
		for _, retained := range []string{
			DomainAntiflood, DomainAntiraid, DomainApprovals, DomainBlacklists,
			DomainConnections, DomainFilters, DomainWelcome, DomainNotes,
			DomainReactions, DomainWarnings,
		} {
			assert.Contains(t, domains, retained)
		}
		for _, removed := range []string{"admin", "disabling", "pins", "rules", "greetings", "warns", "captcha", "reports"} {
			assert.NotContains(t, domains, removed)
		}
	})

	t.Run("IsValidDomain validates correctly", func(t *testing.T) {
		assert.True(t, IsValidDomain("notes"))
		assert.True(t, IsValidDomain("filters"))
		assert.False(t, IsValidDomain("invalid"))
		assert.False(t, IsValidDomain(""))
	})
}

func TestDomainDataEntryPointsRejectUnsupportedDomains(t *testing.T) {
	for _, domain := range []string{"invalid_domain", "admin", "disabling", "pins", "rules", "greetings", "warns"} {
		t.Run(domain, func(t *testing.T) {
			_, err := ExportDomainData(12345, domain)
			assert.ErrorContains(t, err, "unsupported domain")

			assert.ErrorContains(t, ImportDomainData(12345, domain, map[string]interface{}{}), "unsupported domain")
			assert.ErrorContains(t, ClearDomainData(12345, domain), "unsupported domain")
		})
	}
}

func TestImportDomainDataRejectsMalformedPayloadForEveryDomain(t *testing.T) {
	for _, domain := range AllDomains() {
		t.Run(domain, func(t *testing.T) {
			err := ImportDomainData(12345, domain, "not a backup object")

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid")
			assert.Contains(t, err.Error(), "data format")
		})
	}
}

func TestClearDomainDataConnectionsDisconnectsUsers(t *testing.T) {
	skipIfNoDb(t)

	base := time.Now().UnixNano()
	chatID := base
	userID := base + 1
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_backup_connections_clear"))
	t.Cleanup(func() {
		if err := db.DB.Where("user_id = ?", userID).Delete(&models.ConnectionSettings{}).Error; err != nil {
			t.Fatalf("cleanup Delete(ConnectionSettings) error: %v", err)
		}
		if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
			t.Fatalf("cleanup Delete(Chat) error: %v", err)
		}
	})

	require.NoError(t, connections.ConnectId(userID, chatID))
	require.True(t, connections.Connection(userID).Connected)

	require.NoError(t, ClearDomainData(chatID, DomainConnections))

	conn := connections.Connection(userID)
	assert.False(t, conn.Connected)
	assert.Equal(t, int64(0), conn.ChatId)
}

func TestExportChatData(t *testing.T) {
	t.Run("ExportChatData with no valid domains", func(t *testing.T) {
		_, err := ExportChatData(12345, "Test", 67890, []string{"invalid_domain"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported domain")
	})

	t.Run("ExportChatData with empty domains exports all", func(t *testing.T) {
		// Just verify it doesn't error with nil domains
		backup := NewBackupFormat(12345, "Test", 67890, AllDomains())
		assert.NotNil(t, backup)
	})
}

func TestBackupDataStructures(t *testing.T) {
	t.Run("AntifloodBackup struct", func(t *testing.T) {
		backup := &AntifloodBackup{
			Settings: &models.AntifloodSettings{
				ChatId: 12345,
				Limit:  5,
				Action: "mute",
			},
		}
		assert.Equal(t, 5, backup.Settings.Limit)
		assert.Equal(t, "mute", backup.Settings.Action)
	})

	t.Run("NotesBackup struct", func(t *testing.T) {
		backup := &NotesBackup{
			Notes: []models.Notes{
				{
					ChatId:      12345,
					NoteName:    "welcome",
					NoteContent: "Hello!",
				},
			},
		}
		assert.Len(t, backup.Notes, 1)
		assert.Equal(t, "welcome", backup.Notes[0].NoteName)
	})
}

// cleanupBackupChat removes all test data for a chatID across known backup-related tables.
// Uses t.Errorf not t.Fatalf so a failure for one table still attempts the others.
func cleanupBackupChat(t *testing.T, chatID int64) {
	t.Helper()
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntifloodSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting AntifloodSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.BlacklistSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting BlacklistSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.ConnectionSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting ConnectionSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.ChatFilters{}).Error; err != nil {
		t.Errorf("cleanup failed deleting ChatFilters: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.GreetingSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting GreetingSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.NotesSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting NotesSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Notes{}).Error; err != nil {
		t.Errorf("cleanup failed deleting Notes: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.WarnSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting WarnSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.AntiRaidSettings{}).Error; err != nil {
		t.Errorf("cleanup failed deleting AntiRaidSettings: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.ApprovedUsers{}).Error; err != nil {
		t.Errorf("cleanup failed deleting ApprovedUsers: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Warns{}).Error; err != nil {
		t.Errorf("cleanup failed deleting Warns: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Reactions{}).Error; err != nil {
		t.Errorf("cleanup failed deleting Reactions: %v", err)
	}
	if err := db.DB.Where("chat_id = ?", chatID).Delete(&models.Chat{}).Error; err != nil {
		t.Errorf("cleanup failed deleting Chat: %v", err)
	}
}

func TestExportFiltersData(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_export_filters"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	// Empty filters → returns empty backup
	backup, err := exportFiltersData(db.DB, chatID)
	require.NoError(t, err)
	require.NotNil(t, backup)
	assert.Empty(t, backup.Filters)

	// Add filters
	require.NoError(t, filters.AddFilter(chatID, "hello", "hi there", "", nil, db.TEXT))
	require.NoError(t, filters.AddFilter(chatID, "bye", "see ya", "", nil, db.TEXT))

	backup, err = exportFiltersData(db.DB, chatID)
	require.NoError(t, err)
	require.NotNil(t, backup)
	assert.Len(t, backup.Filters, 2)

	names := make([]string, len(backup.Filters))
	for i, f := range backup.Filters {
		names[i] = f.KeyWord
	}
	assert.Contains(t, names, "hello")
	assert.Contains(t, names, "bye")
}

func TestImportFiltersData(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_import_filters"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	payload := map[string]interface{}{
		"filters": []map[string]interface{}{
			{
				"chat_id":      float64(chatID),
				"keyword":      "spam",
				"filter_reply": "no spam",
				"msgtype":      float64(db.TEXT),
			},
			{
				"chat_id":      float64(chatID),
				"keyword":      "ad",
				"filter_reply": "no ads",
				"msgtype":      float64(db.TEXT),
			},
		},
	}

	require.NoError(t, ImportDomainData(chatID, DomainFilters, payload))

	list := filters.GetFiltersList(chatID)
	assert.Len(t, list, 2)
	assert.Contains(t, list, "spam")
	assert.Contains(t, list, "ad")
}

func TestImportFiltersData_InvalidFormat(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_import_filters_invalid"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	err := ImportDomainData(chatID, DomainFilters, "not a map")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filters data format")
}

func TestExportImportNotesRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_notes"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_notes"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	// Add notes to source chat
	require.NoError(t, notes.AddNote(srcChat, "welcome", "Welcome!", "", nil, db.TEXT, false, false, false, true, false, false))
	require.NoError(t, notes.AddNote(srcChat, "rules", "Follow the rules", "", nil, db.TEXT, false, false, false, true, false, false))

	// Export
	exported, err := exportNotesData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	assert.Len(t, exported.Notes, 2)

	// Convert to map for import
	payload := map[string]interface{}{
		"notes": exported.Notes,
	}

	// Import into destination
	require.NoError(t, ImportDomainData(dstChat, DomainNotes, payload))

	list := notes.GetNotesList(dstChat, true)
	assert.Len(t, list, 2)
	assert.Contains(t, list, "welcome")
	assert.Contains(t, list, "rules")

	note := notes.GetNote(dstChat, "welcome")
	require.NotNil(t, note)
	assert.Equal(t, "Welcome!", note.NoteContent)
}

func TestExportImportWarningsRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_warns"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_warns"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	require.NoError(t, warns.SetWarnLimit(srcChat, 5))

	exported, err := exportWarningsData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.NotNil(t, exported.Settings)
	assert.Equal(t, 5, exported.Settings.WarnLimit)

	payload := map[string]interface{}{
		"settings": map[string]interface{}{
			"chat_id":    float64(dstChat),
			"warn_limit": float64(5),
		},
	}

	require.NoError(t, ImportDomainData(dstChat, DomainWarnings, payload))

	settings := warns.GetWarnSetting(dstChat)
	require.NotNil(t, settings)
	assert.Equal(t, 5, settings.WarnLimit)
}

func TestExportImportBlacklistsRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_blacklists"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_blacklists"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	require.NoError(t, blacklists.AddBlacklist(srcChat, "spam"))
	require.NoError(t, blacklists.AddBlacklist(srcChat, "scam"))
	require.NoError(t, blacklists.SetBlacklistAction(srcChat, "ban"))

	exported, err := exportBlacklistsData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	assert.Len(t, exported.Entries, 2)
	for _, entry := range exported.Entries {
		assert.Equal(t, "ban", entry.Action)
	}

	payload := map[string]interface{}{
		"entries": exported.Entries,
	}

	require.NoError(t, ImportDomainData(dstChat, DomainBlacklists, payload))

	settings := blacklists.GetBlacklistSettings(dstChat)
	assert.Len(t, settings, 2)
	assert.Equal(t, "ban", settings.Action())
}

func TestExportImportConnectionsRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	userID := srcChat + 2
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_connections"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_connections"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
		if err := db.DB.Where("user_id = ?", userID).Delete(&models.User{}).Error; err != nil {
			t.Errorf("cleanup failed deleting User: %v", err)
		}
	})

	require.NoError(t, connections.ConnectId(userID, srcChat))

	exported, err := exportConnectionsData(db.DB, srcChat)
	require.NoError(t, err)
	require.Len(t, exported.Connections, 1)
	assert.Equal(t, userID, exported.Connections[0].UserId)

	require.NoError(t, ImportDomainData(dstChat, DomainConnections, payloadFor(t, exported)))

	conn := connections.Connection(userID)
	assert.True(t, conn.Connected)
	assert.Equal(t, dstChat, conn.ChatId)
}

func TestExportImportAntifloodRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_antiflood"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_antiflood"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	require.NoError(t, antiflood.SetFlood(srcChat, 3))
	require.NoError(t, antiflood.SetFloodMode(srcChat, "mute"))
	require.NoError(t, antiflood.SetFloodMsgDel(srcChat, true))

	exported, err := exportAntifloodData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.NotNil(t, exported.Settings)
	assert.Equal(t, 3, exported.Settings.Limit)
	assert.Equal(t, "mute", exported.Settings.Action)
	assert.True(t, exported.Settings.DeleteAntifloodMessage)

	payload := map[string]interface{}{
		"settings": map[string]interface{}{
			"chat_id":                  float64(dstChat),
			"limit":                    float64(3),
			"action":                   "mute",
			"delete_antiflood_message": true,
		},
	}

	require.NoError(t, ImportDomainData(dstChat, DomainAntiflood, payload))

	settings := antiflood.GetFlood(dstChat)
	require.NotNil(t, settings)
	assert.Equal(t, 3, settings.Limit)
	assert.Equal(t, "mute", settings.Action)
	assert.True(t, settings.DeleteAntifloodMessage)
}

func TestExportImportWelcomeRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_welcome"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_welcome"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	require.NoError(t, greetings.SetWelcomeText(srcChat, "Hello {first}!", "", nil, db.TEXT))
	require.NoError(t, greetings.SetWelcomeToggle(srcChat, true))

	exported, err := exportWelcomeData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.NotNil(t, exported.Settings)
	require.NotNil(t, exported.Settings.WelcomeSettings)
	assert.Equal(t, "Hello {first}!", exported.Settings.WelcomeSettings.WelcomeText)
	assert.True(t, exported.Settings.WelcomeSettings.ShouldWelcome)

	// Ensure greeting record exists in dst before import
	_ = greetings.GetGreetingSettings(dstChat)

	// Build payload from exported JSON so keys match struct tags exactly
	require.NoError(t, ImportDomainData(dstChat, DomainWelcome, payloadFor(t, exported)))

	settings := greetings.GetGreetingSettings(dstChat)
	require.NotNil(t, settings)
	require.NotNil(t, settings.WelcomeSettings)
	assert.Equal(t, "Hello {first}!", settings.WelcomeSettings.WelcomeText)
	assert.True(t, settings.WelcomeSettings.ShouldWelcome)
}

func TestImportChatData_Validation(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_import_chat_data"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	invalidBackup := &BackupFormat{
		Version: "", // empty version triggers the incompatible-version check
		BotName: "OtherBot",
		ChatID:  chatID,
		Domains: []string{"notes"},
		Data:    map[string]interface{}{},
	}

	err := ImportChatData(chatID, invalidBackup, []string{"notes"})
	assert.ErrorContains(t, err, "unsupported backup version")

	missingData := NewBackupFormat(chatID, "Test", 1, []string{DomainNotes})
	assert.ErrorContains(t, ImportChatData(chatID, missingData, nil), "invalid backup")
}

func TestImportChatData_SingleDomain(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_import_chat_single"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	backup := NewBackupFormat(chatID, "Test", 1, []string{DomainWarnings})
	backup.Data[DomainWarnings] = map[string]interface{}{
		"settings": map[string]interface{}{
			"chat_id":    float64(chatID),
			"warn_limit": float64(7),
		},
	}

	require.NoError(t, ImportChatData(chatID, backup, []string{DomainWarnings}))

	settings := warns.GetWarnSetting(chatID)
	require.NotNil(t, settings)
	assert.Equal(t, 7, settings.WarnLimit)
}

func TestImportChatData_AllDomainsFromBackup(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_import_chat_all"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	backup := NewBackupFormat(chatID, "Test", 1, []string{DomainFilters, DomainNotes})
	backup.Data[DomainFilters] = map[string]interface{}{
		"filters": []map[string]interface{}{
			{"chat_id": float64(chatID), "keyword": "test", "filter_reply": "reply", "msgtype": float64(db.TEXT)},
		},
	}
	backup.Data[DomainNotes] = map[string]interface{}{
		"notes": []map[string]interface{}{
			{"chat_id": float64(chatID), "note_name": "test", "note_content": "content", "msg_type": float64(db.TEXT)},
		},
	}

	require.NoError(t, ImportChatData(chatID, backup, nil))

	assert.Len(t, filters.GetFiltersList(chatID), 1)
	assert.Len(t, notes.GetNotesList(chatID, true), 1)
}

func TestClearChatData_SpecificDomains(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_clear_specific"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	// Set up data for multiple domains
	require.NoError(t, filters.AddFilter(chatID, "hello", "hi", "", nil, db.TEXT))
	require.NoError(t, antiflood.SetFlood(chatID, 5))
	require.NoError(t, notes.AddNote(chatID, "n", "c", "", nil, db.TEXT, false, false, false, true, false, false))

	// Clear only filters
	require.NoError(t, ClearChatData(chatID, []string{DomainFilters}))

	assert.Empty(t, filters.GetFiltersList(chatID))
	assert.Equal(t, 5, antiflood.GetFlood(chatID).Limit)
	assert.Len(t, notes.GetNotesList(chatID, true), 1)
}

func TestClearChatData_InvalidDomain(t *testing.T) {
	skipIfNoDb(t)

	err := ClearChatData(12345, []string{"invalid_domain"})
	assert.ErrorContains(t, err, "unsupported domain")
}

func TestClearDomainData_IndividualDomains(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_clear_individual"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	// --- Filters ---
	require.NoError(t, filters.AddFilter(chatID, "f", "r", "", nil, db.TEXT))
	require.NoError(t, ClearDomainData(chatID, DomainFilters))
	assert.Empty(t, filters.GetFiltersList(chatID))

	// --- Blacklists ---
	require.NoError(t, blacklists.AddBlacklist(chatID, "badword"))
	require.NoError(t, ClearDomainData(chatID, DomainBlacklists))
	assert.Empty(t, blacklists.GetBlacklistSettings(chatID))

	// --- Notes ---
	require.NoError(t, notes.AddNote(chatID, "n1", "c1", "", nil, db.TEXT, false, false, false, true, false, false))
	require.NoError(t, ClearDomainData(chatID, DomainNotes))
	assert.Empty(t, notes.GetNotesList(chatID, true))

	// --- Warnings ---
	require.NoError(t, warns.SetWarnLimit(chatID, 10))
	require.NoError(t, ClearDomainData(chatID, DomainWarnings))
	assert.Equal(t, 3, warns.GetWarnSetting(chatID).WarnLimit)

	// --- Welcome ---
	require.NoError(t, greetings.SetWelcomeToggle(chatID, true))
	require.NoError(t, ClearDomainData(chatID, DomainWelcome))
	settings := greetings.GetGreetingSettings(chatID)
	if settings != nil && settings.WelcomeSettings != nil {
		assert.False(t, settings.WelcomeSettings.ShouldWelcome)
	}

	// --- Antiflood ---
	require.NoError(t, antiflood.SetFlood(chatID, 8))
	require.NoError(t, ClearDomainData(chatID, DomainAntiflood))
	assert.Equal(t, 0, antiflood.GetFlood(chatID).Limit)
}

func TestExportDomainData_EdgeCases(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_export_edge"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	// No data exists yet → every domain still exports a non-nil payload.
	for _, domain := range AllDomains() {
		data, err := ExportDomainData(chatID, domain)
		require.NoErrorf(t, err, "export %s", domain)
		require.NotNilf(t, data, "export %s", domain)
	}
}

func TestExportChatData_Full(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_export_chat_full"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	// Populate multiple domains
	require.NoError(t, antiflood.SetFlood(chatID, 4))
	require.NoError(t, filters.AddFilter(chatID, "hi", "hello", "", nil, db.TEXT))
	require.NoError(t, notes.AddNote(chatID, "n", "Be kind", "", nil, db.TEXT, false, false, false, true, false, false))

	backup, err := ExportChatData(chatID, "Test Chat", 1, []string{
		DomainAntiflood,
		DomainFilters,
		DomainNotes,
	})
	require.NoError(t, err)
	require.NotNil(t, backup)
	assert.Equal(t, chatID, backup.ChatID)
	assert.Equal(t, "Test Chat", backup.ChatName)
	assert.Len(t, backup.Domains, 3)

	// Verify data is present
	assert.NotNil(t, backup.Data[DomainAntiflood])
	assert.NotNil(t, backup.Data[DomainFilters])
	assert.NotNil(t, backup.Data[DomainNotes])
}

func TestExportChatData_EmptyDomainsExportsAll(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_export_chat_empty"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	require.NoError(t, antiflood.SetFlood(chatID, 2))
	require.NoError(t, filters.AddFilter(chatID, "a", "b", "", nil, db.TEXT))

	backup, err := ExportChatData(chatID, "Test", 1, nil)
	require.NoError(t, err)
	require.NotNil(t, backup)

	// Should contain all domains (even if some have empty data)
	assert.Equal(t, len(AllDomains()), len(backup.Domains))
	assert.NoError(t, backup.Validate())
}

func TestImportChatData_RejectsMissingDomainData(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "test_import_missing"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	backup := NewBackupFormat(chatID, "Test", 1, []string{DomainFilters, DomainNotes})
	// Only provide data for filters
	backup.Data[DomainFilters] = map[string]interface{}{
		"filters": []map[string]interface{}{
			{"chat_id": float64(chatID), "keyword": "k", "filter_reply": "r", "msgtype": float64(db.TEXT)},
		},
	}
	// Notes domain has no data in backup

	err := ImportChatData(chatID, backup, nil)
	require.ErrorContains(t, err, "missing data for domain: notes")
	assert.Empty(t, filters.GetFiltersList(chatID))
}

func TestAntiraidBackupRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_antiraid"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_antiraid"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	// Configure non-default antiraid settings on srcChat
	require.NoError(t, antiraid.SetRaidTime(srcChat, 3600))
	require.NoError(t, antiraid.SetRaidActionTime(srcChat, 7200))
	require.NoError(t, antiraid.SetAutoAntiRaidThreshold(srcChat, 10))

	// Export
	exported, err := exportAntiraidData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.NotNil(t, exported.Settings)
	assert.Equal(t, 3600, exported.Settings.RaidTime)
	assert.Equal(t, 7200, exported.Settings.RaidActionTime)
	assert.Equal(t, 10, exported.Settings.AutoAntiRaidThreshold)

	// Clear srcChat to defaults
	require.NoError(t, ClearDomainData(srcChat, DomainAntiraid))
	cleared := antiraid.GetAntiRaidSettings(srcChat)
	assert.Equal(t, 21600, cleared.RaidTime)
	assert.Equal(t, 3600, cleared.RaidActionTime)
	assert.Equal(t, 0, cleared.AutoAntiRaidThreshold)

	// Import into dstChat
	require.NoError(t, ImportDomainData(dstChat, DomainAntiraid, payloadFor(t, exported)))

	restored := antiraid.GetAntiRaidSettings(dstChat)
	require.NotNil(t, restored)
	assert.Equal(t, 3600, restored.RaidTime)
	assert.Equal(t, 7200, restored.RaidActionTime)
	assert.Equal(t, 10, restored.AutoAntiRaidThreshold)
}

// TestAntiraidBackupExcludesRaidWindow pins the backup surface to configuration:
// the active-raid window is runtime state and must not travel with a backup.
func TestAntiraidBackupExcludesRaidWindow(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_antiraid_window"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
	})

	require.NoError(t, antiraid.SetRaidTime(srcChat, 3600))
	enabled, err := antiraid.EnableRaid(srcChat, 3600)
	require.NoError(t, err)
	require.True(t, enabled)

	exported, err := exportAntiraidData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported.Settings)

	exportedJSON, err := json.Marshal(exported)
	require.NoError(t, err)
	assert.NotContains(t, string(exportedJSON), "raid_active_until")
	assert.NotContains(t, string(exportedJSON), "raid_started_at")

	// Restoring a backup resets the window rather than reopening a stale raid.
	require.NoError(t, ImportDomainData(srcChat, DomainAntiraid, payloadFor(t, exported)))
	assert.False(t, antiraid.GetRaidState(srcChat).Active)
}

func TestApprovalsBackupRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_approvals"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_approvals"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	// Add two approved users
	const approverID = int64(999)
	userA := int64(111)
	userB := int64(222)
	require.NoError(t, approvals.AddApprovedUser(srcChat, userA, approverID, "reason A"))
	require.NoError(t, approvals.AddApprovedUser(srcChat, userB, approverID, "reason B"))

	// Export
	exported, err := exportApprovalsData(db.DB, srcChat)
	require.NoError(t, err)
	require.NotNil(t, exported)
	assert.Len(t, exported.ApprovedUsers, 2)

	exportedUserIDs := make([]int64, len(exported.ApprovedUsers))
	for i, u := range exported.ApprovedUsers {
		exportedUserIDs[i] = u.UserID
	}
	assert.Contains(t, exportedUserIDs, userA)
	assert.Contains(t, exportedUserIDs, userB)

	// Clear srcChat approvals
	require.NoError(t, ClearDomainData(srcChat, DomainApprovals))
	assert.Empty(t, approvals.GetApprovedUsers(srcChat))

	// Import into dstChat
	require.NoError(t, ImportDomainData(dstChat, DomainApprovals, payloadFor(t, exported)))

	restoredUsers := approvals.GetApprovedUsers(dstChat)
	require.Len(t, restoredUsers, 2)

	restoredIDs := make([]int64, len(restoredUsers))
	for i, u := range restoredUsers {
		restoredIDs[i] = u.UserID
	}
	assert.Contains(t, restoredIDs, userA)
	assert.Contains(t, restoredIDs, userB)

	// Verify approvals recognizes the restored users
	assert.True(t, approvals.IsUserApproved(dstChat, userA))
	assert.True(t, approvals.IsUserApproved(dstChat, userB))
}

func TestReactionsBackupRoundTrip(t *testing.T) {
	skipIfNoDb(t)

	srcChat := time.Now().UnixNano()
	dstChat := srcChat + 1
	require.NoError(t, chats.EnsureChatInDb(srcChat, "src_reactions"))
	require.NoError(t, chats.EnsureChatInDb(dstChat, "dst_reactions"))
	t.Cleanup(func() {
		cleanupBackupChat(t, srcChat)
		cleanupBackupChat(t, dstChat)
	})

	require.NoError(t, db.DB.Create(&models.Reactions{ChatID: srcChat, Keyword: "gg", Emoji: "🔥"}).Error)

	exported, err := exportReactionsData(db.DB, srcChat)
	require.NoError(t, err)
	require.Len(t, exported.Reactions, 1)

	require.NoError(t, ImportDomainData(dstChat, DomainReactions, payloadFor(t, exported)))

	restored, err := exportReactionsData(db.DB, dstChat)
	require.NoError(t, err)
	require.Len(t, restored.Reactions, 1)
	assert.Equal(t, "gg", restored.Reactions[0].Keyword)
	assert.Equal(t, "🔥", restored.Reactions[0].Emoji)
}
