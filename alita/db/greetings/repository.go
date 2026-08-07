package greetings

import (
	"errors"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
	alitaerrors "github.com/divkix/Alita_Robot/alita/utils/errors"
)

// checkGreetingSettings retrieves or creates default greeting settings for a chat.
// Used internally before performing any greeting-related operation.
// Returns default settings if the chat doesn't exist in the database.
func checkGreetingSettings(chatID int64) (greetingSrc *models.GreetingSettings) {
	greetingSrc = &models.GreetingSettings{}
	err := db.GetRecord(greetingSrc, map[string]any{"chat_id": chatID})

	if errors.Is(err, gorm.ErrRecordNotFound) || err != nil || greetingSrc.WelcomeSettings == nil {
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorf("[Database][checkGreetingSettings]: %v", err)
		}
		greetingSrc = &models.GreetingSettings{
			ChatID:             chatID,
			ShouldCleanService: false,
			WelcomeSettings: &models.WelcomeSettings{
				LastMsgId:     0,
				CleanWelcome:  false,
				ShouldWelcome: true,
				WelcomeText:   db.DefaultWelcome,
				WelcomeType:   db.TEXT,
				Button:        models.ButtonArray{},
			},
		}
		return greetingSrc
	}

	if greetingSrc.WelcomeSettings.WelcomeText == "" {
		greetingSrc.WelcomeSettings.WelcomeText = db.DefaultWelcome
	}

	return greetingSrc
}

// GetGreetingSettings returns the greeting settings for the specified chat ID.
// This is the public interface to access greeting settings.
func GetGreetingSettings(chatID int64) *models.GreetingSettings {
	return checkGreetingSettings(chatID)
}

// GetWelcomeButtons retrieves the welcome message buttons for the specified chat.
// Returns an empty slice if no buttons are configured or settings are missing.
func GetWelcomeButtons(chatId int64) []models.Button {
	greetingSettings := checkGreetingSettings(chatId)
	if greetingSettings.WelcomeSettings != nil && len(greetingSettings.WelcomeSettings.Button) > 0 {
		return []models.Button(greetingSettings.WelcomeSettings.Button)
	}
	return []models.Button{}
}

func upsertGreetingSettings(chatID int64, updates map[string]any) error {
	if !db.ChatExists(chatID) {
		if err := chats.EnsureChatInDb(chatID, ""); err != nil {
			return alitaerrors.Wrapf(err, "ensure chat %d in db", chatID)
		}
	}
	now := time.Now().UTC()
	row := map[string]any{
		"chat_id":                chatID,
		"clean_service_settings": false,
		"welcome_clean_old":      false,
		"welcome_last_msg_id":    int64(0),
		"welcome_enabled":        true,
		"welcome_text":           db.DefaultWelcome,
		"welcome_file_id":        "",
		"welcome_type":           db.TEXT,
		"welcome_btns":           models.ButtonArray{},
		"created_at":             now,
		"updated_at":             now,
	}

	assigned := make([]string, 0, len(updates)+1)
	for column, value := range updates {
		if column == "chat_id" {
			continue
		}
		row[column] = value
		assigned = append(assigned, column)
	}
	sort.Strings(assigned)
	assigned = append(assigned, "updated_at")

	err := db.RetryOnLock(func() error {
		return db.DB.Model(&models.GreetingSettings{}).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}},
			DoUpdates: clause.AssignmentColumns(assigned),
		}).Create(row).Error
	})
	if err != nil {
		return alitaerrors.Wrapf(err, "upsert greeting settings for chat %d", chatID)
	}
	return nil
}

// SetWelcomeText updates the welcome message text, file ID, buttons, and type for a chat.
// Creates default greeting settings if they don't exist.
func SetWelcomeText(chatID int64, welcometxt, fileId string, buttons []models.Button, welcType int) error {
	updates := map[string]any{
		"welcome_text":    welcometxt,
		"welcome_btns":    models.ButtonArray(buttons),
		"welcome_type":    welcType,
		"welcome_file_id": fileId,
	}

	err := upsertGreetingSettings(chatID, updates)
	if err != nil {
		log.Errorf("[Database][SetWelcomeText]: %v", err)
		return err
	}

	// Invalidate cache after updating welcome text
	cache.DeleteCache(cache.CacheKey("greetings", chatID))
	return nil
}

// SetWelcomeToggle enables or disables welcome messages for the specified chat.
// Creates default greeting settings if they don't exist.
func SetWelcomeToggle(chatID int64, pref bool) error {
	updates := map[string]any{
		"welcome_enabled": pref,
	}

	err := upsertGreetingSettings(chatID, updates)
	if err != nil {
		log.Errorf("[Database][SetWelcomeToggle]: %v", err)
		return err
	}

	// Invalidate cache after updating welcome toggle
	cache.DeleteCache(cache.CacheKey("greetings", chatID))
	return nil
}

// SetShouldCleanService sets whether service messages should be automatically cleaned in the chat.
// Creates default greeting settings if they don't exist.
func SetShouldCleanService(chatID int64, pref bool) error {
	updates := map[string]any{
		"clean_service_settings": pref,
	}

	err := upsertGreetingSettings(chatID, updates)
	if err != nil {
		log.Errorf("[Database][SetShouldCleanService]: %v", err)
		return err
	}

	// Invalidate cache after updating clean service setting
	cache.DeleteCache(cache.CacheKey("greetings", chatID))
	return nil
}

// SetCleanWelcomeSetting sets whether old welcome messages should be automatically cleaned.
// Creates default greeting settings if they don't exist.
func SetCleanWelcomeSetting(chatID int64, pref bool) error {
	updates := map[string]any{
		"welcome_clean_old": pref,
	}

	err := upsertGreetingSettings(chatID, updates)
	if err != nil {
		log.Errorf("[Database][SetCleanWelcomeSetting]: %v", err)
		return err
	}

	// Invalidate cache after updating clean welcome setting
	cache.DeleteCache(cache.CacheKey("greetings", chatID))
	return nil
}

// SetCleanWelcomeMsgId updates the message ID of the last welcome message for cleanup purposes.
// Creates default greeting settings if they don't exist.
func SetCleanWelcomeMsgId(chatId, msgId int64) error {
	updates := map[string]any{
		"welcome_last_msg_id": msgId,
	}

	err := upsertGreetingSettings(chatId, updates)
	if err != nil {
		log.Errorf("[Database][SetCleanWelcomeMsgId]: %v", err)
		return err
	}

	// Invalidate cache after updating welcome message ID
	cache.DeleteCache(cache.CacheKey("greetings", chatId))
	return nil
}

// LoadGreetingsStats returns statistics about retained greeting features across all chats.
func LoadGreetingsStats() (enabledWelcome, cleanServiceEnabled, cleanWelcomeEnabled int64) {
	// Use a single query with COUNT and CASE WHEN for better performance
	type greetingStats struct {
		EnabledWelcome      int64
		CleanServiceEnabled int64
		CleanWelcomeEnabled int64
	}

	var stats greetingStats
	query := `
		SELECT
			COUNT(CASE WHEN welcome_enabled = true THEN 1 END) as enabled_welcome,
			COUNT(CASE WHEN clean_service_settings = true THEN 1 END) as clean_service_enabled,
			COUNT(CASE WHEN welcome_clean_old = true THEN 1 END) as clean_welcome_enabled
		FROM greetings
	`

	err := db.DB.Raw(query).Scan(&stats).Error
	if err != nil {
		log.Errorf("[Database][LoadGreetingsStats] querying stats: %v", err)
		return 0, 0, 0
	}

	return stats.EnabledWelcome, stats.CleanServiceEnabled, stats.CleanWelcomeEnabled
}
