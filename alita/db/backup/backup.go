package backup

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/divkix/Alita_Robot/alita/db"
	dbcache "github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExportDomainData exports data for a single retained domain from a chat.
func ExportDomainData(chatID int64, domain string) (interface{}, error) {
	database, err := backupDB()
	if err != nil {
		return nil, err
	}
	return exportDomainData(database, chatID, domain)
}

func exportDomainData(tx *gorm.DB, chatID int64, domain string) (interface{}, error) {
	switch domain {
	case DomainAntiflood:
		return exportAntifloodData(tx, chatID)
	case DomainAntiraid:
		return exportAntiraidData(tx, chatID)
	case DomainApprovals:
		return exportApprovalsData(tx, chatID)
	case DomainBlacklists:
		return exportBlacklistsData(tx, chatID)
	case DomainConnections:
		return exportConnectionsData(tx, chatID)
	case DomainFilters:
		return exportFiltersData(tx, chatID)
	case DomainWelcome:
		return exportWelcomeData(tx, chatID)
	case DomainNotes:
		return exportNotesData(tx, chatID)
	case DomainReactions:
		return exportReactionsData(tx, chatID)
	case DomainWarnings:
		return exportWarningsData(tx, chatID)
	default:
		return nil, fmt.Errorf("unsupported domain: %s", domain)
	}
}

// ImportDomainData imports one domain atomically into a chat.
func ImportDomainData(chatID int64, domain string, data interface{}) error {
	if !IsValidDomain(domain) {
		return fmt.Errorf("unsupported domain: %s", domain)
	}
	database, err := backupDB()
	if err != nil {
		return err
	}

	var keys []string
	err = database.Transaction(func(tx *gorm.DB) error {
		var importErr error
		keys, importErr = importDomainData(tx, chatID, domain, data)
		return importErr
	})
	if err != nil {
		return err
	}
	invalidate(keys...)
	return nil
}

// ClearDomainData clears one domain atomically from a chat.
func ClearDomainData(chatID int64, domain string) error {
	if !IsValidDomain(domain) {
		return fmt.Errorf("unsupported domain: %s", domain)
	}
	database, err := backupDB()
	if err != nil {
		return err
	}

	var keys []string
	err = database.Transaction(func(tx *gorm.DB) error {
		var clearErr error
		keys, clearErr = clearDomainData(tx, chatID, domain)
		return clearErr
	})
	if err != nil {
		return err
	}
	invalidate(keys...)
	return nil
}

// ExportChatData exports the selected domains inside one read transaction so a
// concurrent write cannot produce a torn backup. A failed domain aborts the
// export so callers never receive a backup that only looks complete.
func ExportChatData(chatID int64, chatName string, exportedBy int64, domains []string) (*BackupFormat, error) {
	domains, err := checkedDomains(domains)
	if err != nil {
		return nil, err
	}

	database, err := backupDB()
	if err != nil {
		return nil, err
	}

	backup := NewBackupFormat(chatID, chatName, exportedBy, domains)
	err = database.Transaction(func(tx *gorm.DB) error {
		for _, domain := range domains {
			data, exportErr := exportDomainData(tx, chatID, domain)
			if exportErr != nil {
				return fmt.Errorf("failed to export domain %s: %w", domain, exportErr)
			}
			backup.Data[domain] = data
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return backup, nil
}

// ImportChatData imports every selected domain in one transaction. Unknown and
// removed domain names are rejected before any row is written, and a failure in
// any domain rolls back the changes made by the earlier ones.
func ImportChatData(chatID int64, backup *BackupFormat, domains []string) error {
	if backup == nil {
		return fmt.Errorf("invalid backup: backup cannot be nil")
	}
	if !backup.IsCompatibleVersion() {
		return fmt.Errorf("unsupported backup version %q", backup.Version)
	}
	if err := backup.Validate(); err != nil {
		return fmt.Errorf("invalid backup: %w", err)
	}
	if len(domains) == 0 {
		domains = backup.Domains
	}
	for _, domain := range domains {
		if !IsValidDomain(domain) {
			return fmt.Errorf("unsupported domain: %s", domain)
		}
		if _, ok := backup.Data[domain]; !ok {
			return fmt.Errorf("missing data for domain: %s", domain)
		}
	}

	database, err := backupDB()
	if err != nil {
		return err
	}

	var keys []string
	err = database.Transaction(func(tx *gorm.DB) error {
		keys = nil
		for _, domain := range domains {
			domainKeys, err := importDomainData(tx, chatID, domain, backup.Data[domain])
			if err != nil {
				return fmt.Errorf("failed to import domain %s: %w", domain, err)
			}
			keys = append(keys, domainKeys...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	invalidate(keys...)
	return nil
}

// ClearChatData clears every selected domain in one transaction.
func ClearChatData(chatID int64, domains []string) error {
	domains, err := checkedDomains(domains)
	if err != nil {
		return err
	}
	database, err := backupDB()
	if err != nil {
		return err
	}

	var keys []string
	err = database.Transaction(func(tx *gorm.DB) error {
		keys = nil
		for _, domain := range domains {
			domainKeys, err := clearDomainData(tx, chatID, domain)
			if err != nil {
				return fmt.Errorf("failed to clear domain %s: %w", domain, err)
			}
			keys = append(keys, domainKeys...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	invalidate(keys...)
	return nil
}

func checkedDomains(domains []string) ([]string, error) {
	if len(domains) == 0 {
		return AllDomains(), nil
	}
	for _, domain := range domains {
		if !IsValidDomain(domain) {
			return nil, fmt.Errorf("unsupported domain: %s", domain)
		}
	}
	return domains, nil
}

func backupDB() (*gorm.DB, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return db.DB, nil
}

func findChatSetting[T any](tx *gorm.DB, chatID int64) (*T, error) {
	var setting T
	err := tx.Where("chat_id = ?", chatID).Take(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func findChatRows[T any](tx *gorm.DB, chatID int64) ([]T, error) {
	var rows []T
	if err := tx.Where("chat_id = ?", chatID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func replaceChatSetting[T any](tx *gorm.DB, chatID int64, setting *T) error {
	if err := tx.Where("chat_id = ?", chatID).Delete(new(T)).Error; err != nil {
		return err
	}
	if setting == nil {
		return nil
	}
	var desired T
	raw, err := json.Marshal(setting)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &desired); err != nil {
		return err
	}
	if err := tx.Omit("ID").Create(setting).Error; err != nil {
		return err
	}
	// GORM applies tagged database defaults to zero-valued fields on CREATE.
	// The explicit update is required for backed-up false/0 values.
	return tx.Model(new(T)).
		Where("chat_id = ?", chatID).
		Select("*").
		Omit("ID", "CreatedAt").
		Updates(&desired).Error
}

func replaceChatRows[T any](tx *gorm.DB, chatID int64, rows []T) error {
	if err := tx.Where("chat_id = ?", chatID).Delete(new(T)).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	var desired []T
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &desired); err != nil {
		return err
	}
	if err := tx.Omit("ID").Create(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		if err := tx.Model(&rows[i]).
			Select("*").
			Omit("ID", "CreatedAt").
			Updates(&desired[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func decodeDomainData(data interface{}, domain string, target interface{}) error {
	if _, ok := data.(map[string]interface{}); !ok {
		return fmt.Errorf("invalid %s data format", domain)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("invalid %s data format: %w", domain, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("failed to parse %s data: %w", domain, err)
	}
	return nil
}

func invalidate(keys ...string) {
	for _, key := range keys {
		dbcache.DeleteCache(key)
	}
}

func cacheKey(domain string, chatID int64) string {
	return dbcache.CacheKey(domain, chatID)
}

// ensureUsers creates placeholder user rows so imported rows that reference a
// user satisfy the users foreign key.
func ensureUsers(tx *gorm.DB, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(userIDs))
	users := make([]models.User, 0, len(userIDs))
	for _, userID := range userIDs {
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		users = append(users, models.User{UserId: userID})
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(&users).Error
}

// Exporters query complete rows instead of lossy summary getters.

func exportAntifloodData(tx *gorm.DB, chatID int64) (*AntifloodBackup, error) {
	settings, err := findChatSetting[models.AntifloodSettings](tx, chatID)
	return &AntifloodBackup{Settings: settings}, err
}

func exportAntiraidData(tx *gorm.DB, chatID int64) (*AntiraidBackup, error) {
	settings, err := findChatSetting[models.AntiRaidSettings](tx, chatID)
	return &AntiraidBackup{Settings: settings}, err
}

func exportApprovalsData(tx *gorm.DB, chatID int64) (*ApprovalsBackup, error) {
	users, err := findChatRows[models.ApprovedUsers](tx, chatID)
	return &ApprovalsBackup{ApprovedUsers: users}, err
}

func exportBlacklistsData(tx *gorm.DB, chatID int64) (*BlacklistsBackup, error) {
	entries, err := findChatRows[models.BlacklistSettings](tx, chatID)
	return &BlacklistsBackup{Entries: entries}, err
}

func exportConnectionsData(tx *gorm.DB, chatID int64) (*ConnectionsBackup, error) {
	rows, err := findChatRows[models.ConnectionSettings](tx, chatID)
	return &ConnectionsBackup{Connections: rows}, err
}

func exportFiltersData(tx *gorm.DB, chatID int64) (*FiltersBackup, error) {
	rows, err := findChatRows[models.ChatFilters](tx, chatID)
	return &FiltersBackup{Filters: rows}, err
}

func exportWelcomeData(tx *gorm.DB, chatID int64) (*WelcomeBackup, error) {
	settings, err := findChatSetting[models.GreetingSettings](tx, chatID)
	return &WelcomeBackup{Settings: settings}, err
}

func exportNotesData(tx *gorm.DB, chatID int64) (*NotesBackup, error) {
	settings, err := findChatSetting[models.NotesSettings](tx, chatID)
	if err != nil {
		return nil, err
	}
	rows, err := findChatRows[models.Notes](tx, chatID)
	if err != nil {
		return nil, err
	}
	return &NotesBackup{Settings: settings, Notes: rows}, nil
}

func exportReactionsData(tx *gorm.DB, chatID int64) (*ReactionsBackup, error) {
	rows, err := findChatRows[models.Reactions](tx, chatID)
	return &ReactionsBackup{Reactions: rows}, err
}

func exportWarningsData(tx *gorm.DB, chatID int64) (*WarningsBackup, error) {
	settings, err := findChatSetting[models.WarnSettings](tx, chatID)
	if err != nil {
		return nil, err
	}
	rows, err := findChatRows[models.Warns](tx, chatID)
	if err != nil {
		return nil, err
	}
	return &WarningsBackup{Settings: settings, Warns: rows}, nil
}

func importDomainData(tx *gorm.DB, chatID int64, domain string, data interface{}) ([]string, error) {
	if err := ensureBackupChat(tx, chatID); err != nil {
		return nil, err
	}
	switch domain {
	case DomainAntiflood:
		return importAntiflood(tx, chatID, data)
	case DomainAntiraid:
		return importAntiraid(tx, chatID, data)
	case DomainApprovals:
		return importApprovals(tx, chatID, data)
	case DomainBlacklists:
		return importBlacklists(tx, chatID, data)
	case DomainConnections:
		return importConnections(tx, chatID, data)
	case DomainFilters:
		return importFilters(tx, chatID, data)
	case DomainWelcome:
		return importWelcome(tx, chatID, data)
	case DomainNotes:
		return importNotes(tx, chatID, data)
	case DomainReactions:
		return importReactions(tx, chatID, data)
	case DomainWarnings:
		return importWarnings(tx, chatID, data)
	default:
		return nil, fmt.Errorf("unsupported domain: %s", domain)
	}
}

func importAntiflood(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data AntifloodBackup
	if err := decodeDomainData(payload, DomainAntiflood, &data); err != nil {
		return nil, err
	}
	if data.Settings != nil {
		data.Settings.ChatId = chatID
		if data.Settings.Limit < 0 {
			return nil, fmt.Errorf("invalid antiflood limit %d", data.Settings.Limit)
		}
	}
	if err := replaceChatSetting(tx, chatID, data.Settings); err != nil {
		return nil, err
	}
	return []string{cacheKey("antiflood", chatID)}, nil
}

func importAntiraid(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data AntiraidBackup
	if err := decodeDomainData(payload, DomainAntiraid, &data); err != nil {
		return nil, err
	}
	if data.Settings != nil {
		data.Settings.ChatID = chatID
		if data.Settings.RaidTime < 0 || data.Settings.RaidActionTime < 0 || data.Settings.AutoAntiRaidThreshold < 0 {
			return nil, fmt.Errorf("invalid antiraid settings")
		}
	}
	if err := replaceChatSetting(tx, chatID, data.Settings); err != nil {
		return nil, err
	}
	return []string{cacheKey("antiraid", chatID)}, nil
}

func importApprovals(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data ApprovalsBackup
	if err := decodeDomainData(payload, DomainApprovals, &data); err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(data.ApprovedUsers))
	for i := range data.ApprovedUsers {
		if data.ApprovedUsers[i].UserID == 0 {
			return nil, fmt.Errorf("invalid approved user ID")
		}
		data.ApprovedUsers[i].ChatID = chatID
		userIDs = append(userIDs, data.ApprovedUsers[i].UserID)
	}
	if err := ensureUsers(tx, userIDs); err != nil {
		return nil, fmt.Errorf("ensure approved users: %w", err)
	}
	if err := replaceChatRows(tx, chatID, data.ApprovedUsers); err != nil {
		return nil, err
	}
	return []string{cacheKey("approvals", chatID)}, nil
}

func importBlacklists(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) { //nolint:dupl // domain-specific schema
	var data BlacklistsBackup
	if err := decodeDomainData(payload, DomainBlacklists, &data); err != nil {
		return nil, err
	}
	for i := range data.Entries {
		if data.Entries[i].Word == "" {
			return nil, fmt.Errorf("invalid empty blacklist word")
		}
		data.Entries[i].ChatId = chatID
		if data.Entries[i].Action == "" {
			data.Entries[i].Action = "warn"
		}
	}
	if err := replaceChatRows(tx, chatID, data.Entries); err != nil {
		return nil, err
	}
	return []string{cacheKey("blacklist", chatID)}, nil
}

func importConnections(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data ConnectionsBackup
	if err := decodeDomainData(payload, DomainConnections, &data); err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(data.Connections))
	for i := range data.Connections {
		if data.Connections[i].UserId == 0 {
			return nil, fmt.Errorf("invalid connection user ID")
		}
		data.Connections[i].ChatId = chatID
		userIDs = append(userIDs, data.Connections[i].UserId)
	}
	if err := ensureUsers(tx, userIDs); err != nil {
		return nil, fmt.Errorf("ensure connected users: %w", err)
	}
	// A user may only hold one connection, so any connection an imported user
	// already has to another chat is dropped before the chat rows are replaced.
	if len(userIDs) > 0 {
		if err := tx.Where("user_id IN ?", userIDs).Delete(&models.ConnectionSettings{}).Error; err != nil {
			return nil, err
		}
	}
	if err := replaceChatRows(tx, chatID, data.Connections); err != nil {
		return nil, err
	}
	return nil, nil
}

func importFilters(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) { //nolint:dupl // domain-specific schema
	var data FiltersBackup
	if err := decodeDomainData(payload, DomainFilters, &data); err != nil {
		return nil, err
	}
	for i := range data.Filters {
		if data.Filters[i].KeyWord == "" {
			return nil, fmt.Errorf("invalid empty filter keyword")
		}
		data.Filters[i].ChatId = chatID
	}
	if err := replaceChatRows(tx, chatID, data.Filters); err != nil {
		return nil, err
	}
	return []string{
		cacheKey("filter_list", chatID),
		cacheKey("filters_optimized", chatID),
	}, nil
}

func importWelcome(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data WelcomeBackup
	if err := decodeDomainData(payload, DomainWelcome, &data); err != nil {
		return nil, err
	}
	if data.Settings != nil {
		data.Settings.ChatID = chatID
	}
	if err := replaceChatSetting(tx, chatID, data.Settings); err != nil {
		return nil, err
	}
	return []string{cacheKey("greetings", chatID)}, nil
}

func importNotes(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data NotesBackup
	if err := decodeDomainData(payload, DomainNotes, &data); err != nil {
		return nil, err
	}
	if data.Settings != nil {
		data.Settings.ChatId = chatID
	}
	for i := range data.Notes {
		if data.Notes[i].NoteName == "" {
			return nil, fmt.Errorf("invalid empty note name")
		}
		data.Notes[i].ChatId = chatID
	}
	if err := replaceChatSetting(tx, chatID, data.Settings); err != nil {
		return nil, err
	}
	if err := replaceChatRows(tx, chatID, data.Notes); err != nil {
		return nil, err
	}
	return []string{cacheKey("notes_settings", chatID)}, nil
}

func importReactions(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data ReactionsBackup
	if err := decodeDomainData(payload, DomainReactions, &data); err != nil {
		return nil, err
	}
	for i := range data.Reactions {
		if data.Reactions[i].Keyword == "" || data.Reactions[i].Emoji == "" {
			return nil, fmt.Errorf("invalid reaction")
		}
		data.Reactions[i].ChatID = chatID
	}
	if err := replaceChatRows(tx, chatID, data.Reactions); err != nil {
		return nil, err
	}
	return []string{cacheKey("reactions", chatID)}, nil
}

func importWarnings(tx *gorm.DB, chatID int64, payload interface{}) ([]string, error) {
	var data WarningsBackup
	if err := decodeDomainData(payload, DomainWarnings, &data); err != nil {
		return nil, err
	}
	if data.Settings != nil {
		data.Settings.ChatId = chatID
		if data.Settings.WarnLimit <= 0 {
			return nil, fmt.Errorf("invalid warn limit %d", data.Settings.WarnLimit)
		}
	}
	userIDs := make([]int64, 0, len(data.Warns))
	for i := range data.Warns {
		if data.Warns[i].UserId == 0 {
			return nil, fmt.Errorf("invalid warn record")
		}
		data.Warns[i].ChatId = chatID
		userIDs = append(userIDs, data.Warns[i].UserId)
	}
	if err := ensureUsers(tx, userIDs); err != nil {
		return nil, fmt.Errorf("ensure warned users: %w", err)
	}

	var oldUserIDs []int64
	if err := tx.Model(&models.Warns{}).Where("chat_id = ?", chatID).Pluck("user_id", &oldUserIDs).Error; err != nil {
		return nil, err
	}
	if err := replaceChatSetting(tx, chatID, data.Settings); err != nil {
		return nil, err
	}
	if err := replaceChatRows(tx, chatID, data.Warns); err != nil {
		return nil, err
	}

	keys := []string{cacheKey("warn_settings", chatID)}
	for _, userID := range oldUserIDs {
		keys = append(keys, dbcache.CacheKey("warns", userID, chatID))
	}
	for _, row := range data.Warns {
		keys = append(keys, dbcache.CacheKey("warns", row.UserId, chatID))
	}
	return keys, nil
}

func clearDomainData(tx *gorm.DB, chatID int64, domain string) ([]string, error) {
	if err := ensureBackupChat(tx, chatID); err != nil {
		return nil, err
	}
	switch domain {
	case DomainAntiflood:
		return clearAntiflood(tx, chatID)
	case DomainAntiraid:
		return clearAntiraid(tx, chatID)
	case DomainApprovals:
		return clearApprovals(tx, chatID)
	case DomainBlacklists:
		return clearBlacklists(tx, chatID)
	case DomainConnections:
		return clearConnections(tx, chatID)
	case DomainFilters:
		return clearFilters(tx, chatID)
	case DomainWelcome:
		return clearWelcome(tx, chatID)
	case DomainNotes:
		return clearNotes(tx, chatID)
	case DomainReactions:
		return clearReactions(tx, chatID)
	case DomainWarnings:
		return clearWarnings(tx, chatID)
	default:
		return nil, fmt.Errorf("unsupported domain: %s", domain)
	}
}

func ensureBackupChat(tx *gorm.DB, chatID int64) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chat_id"}},
		DoNothing: true,
	}).Create(&models.Chat{ChatId: chatID}).Error
}

func clearAntiflood(tx *gorm.DB, chatID int64) ([]string, error) {
	settings := &models.AntifloodSettings{ChatId: chatID, Limit: 0, Action: "mute"}
	return []string{cacheKey("antiflood", chatID)}, replaceChatSetting(tx, chatID, settings)
}

func clearAntiraid(tx *gorm.DB, chatID int64) ([]string, error) {
	settings := &models.AntiRaidSettings{
		ChatID:                chatID,
		RaidTime:              21600,
		RaidActionTime:        3600,
		AutoAntiRaidThreshold: 0,
	}
	return []string{cacheKey("antiraid", chatID)}, replaceChatSetting(tx, chatID, settings)
}

func clearApprovals(tx *gorm.DB, chatID int64) ([]string, error) {
	return []string{cacheKey("approvals", chatID)}, replaceChatRows[models.ApprovedUsers](tx, chatID, nil)
}

func clearBlacklists(tx *gorm.DB, chatID int64) ([]string, error) {
	return []string{cacheKey("blacklist", chatID)}, replaceChatRows[models.BlacklistSettings](tx, chatID, nil)
}

func clearConnections(tx *gorm.DB, chatID int64) ([]string, error) {
	return nil, tx.Where("chat_id = ?", chatID).Delete(&models.ConnectionSettings{}).Error
}

func clearFilters(tx *gorm.DB, chatID int64) ([]string, error) {
	return []string{
		cacheKey("filter_list", chatID),
		cacheKey("filters_optimized", chatID),
	}, replaceChatRows[models.ChatFilters](tx, chatID, nil)
}

func clearWelcome(tx *gorm.DB, chatID int64) ([]string, error) {
	settings := &models.GreetingSettings{
		ChatID: chatID,
		WelcomeSettings: &models.WelcomeSettings{
			WelcomeText: db.DefaultWelcome,
			WelcomeType: db.TEXT,
			Button:      models.ButtonArray{},
		},
	}
	return []string{cacheKey("greetings", chatID)}, replaceChatSetting(tx, chatID, settings)
}

func clearNotes(tx *gorm.DB, chatID int64) ([]string, error) {
	if err := replaceChatSetting(tx, chatID, &models.NotesSettings{ChatId: chatID}); err != nil {
		return nil, err
	}
	return []string{cacheKey("notes_settings", chatID)}, replaceChatRows[models.Notes](tx, chatID, nil)
}

func clearReactions(tx *gorm.DB, chatID int64) ([]string, error) {
	return []string{cacheKey("reactions", chatID)}, replaceChatRows[models.Reactions](tx, chatID, nil)
}

func clearWarnings(tx *gorm.DB, chatID int64) ([]string, error) {
	var userIDs []int64
	if err := tx.Model(&models.Warns{}).Where("chat_id = ?", chatID).Pluck("user_id", &userIDs).Error; err != nil {
		return nil, err
	}
	if err := replaceChatSetting(tx, chatID, &models.WarnSettings{ChatId: chatID, WarnLimit: 3}); err != nil {
		return nil, err
	}
	if err := replaceChatRows[models.Warns](tx, chatID, nil); err != nil {
		return nil, err
	}
	keys := []string{cacheKey("warn_settings", chatID)}
	for _, userID := range userIDs {
		keys = append(keys, dbcache.CacheKey("warns", userID, chatID))
	}
	return keys, nil
}
