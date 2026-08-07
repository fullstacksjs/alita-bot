package chats

import (
	"errors"
	"fmt"
	"time"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/models"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetInactivityThreshold returns the duration after which a chat is considered inactive.
func GetInactivityThreshold() time.Duration {
	days := 30
	if config.AppConfig != nil && config.AppConfig.InactivityThresholdDays > 0 {
		days = config.AppConfig.InactivityThresholdDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// IsChatActive determines whether a chat is active based on last_activity and is_inactive flag.
func IsChatActive(chat *models.Chat) bool {
	if chat == nil {
		return false
	}
	if chat.IsInactive {
		return false
	}
	if chat.LastActivity.IsZero() {
		return true
	}
	cutoff := time.Now().Add(-GetInactivityThreshold())
	return !chat.LastActivity.Before(cutoff)
}

// GetChatSettings retrieves chat settings using optimized cached queries.
// Returns an empty Chat struct if not found or on error.
func GetChatSettings(chatId int64) (chatSrc *models.Chat) {
	// Use optimized cached query instead of SELECT *
	chat, err := GetChatBasicInfoCached(chatId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &models.Chat{}
		}
		log.Errorf("[Database] GetChatSettings: %v - %d", err, chatId)
		return &models.Chat{}
	}
	return chat
}

// EnsureChatInDb ensures that a chat exists in the database.
// Creates the chat record if it doesn't exist, or updates it if it does.
// This is essential for foreign key constraints that reference the chats table.
func EnsureChatInDb(chatId int64, chatName string) error {
	chatUpdate := &models.Chat{
		ChatId:   chatId,
		ChatName: chatName,
	}
	onConflict := clause.OnConflict{
		Columns:   []clause.Column{{Name: "chat_id"}},
		DoNothing: true,
	}
	if chatName != "" {
		onConflict.DoNothing = false
		onConflict.DoUpdates = clause.AssignmentColumns([]string{"chat_name", "updated_at"})
	}
	var err error
	err = db.RetryOnLock(func() error {
		return db.DB.Clauses(onConflict).Create(chatUpdate).Error
	})
	if err != nil {
		log.Errorf("[Database] EnsureChatInDb: %v", err)
		return fmt.Errorf("failed to ensure chat %d in database: %w", chatId, err)
	}
	cache.DeleteCache(cache.CacheKey("chat", chatId))
	return nil
}

// UpdateChat updates or creates a chat record with the given information.
// Adds user to the chat's user list atomically if not already present, marks chat as active,
// and updates the last activity timestamp to track when messages are received.
// Returns error if database operation fails.
func UpdateChat(chatId int64, chatname string, userid int64) error {
	now := time.Now()

	columns := []string{"is_inactive", "last_activity", "updated_at"}
	if chatname != "" {
		columns = append(columns, "chat_name")
	}
	chat := &models.Chat{
		ChatId:       chatId,
		ChatName:     chatname,
		Users:        models.Int64Array{userid},
		IsInactive:   false,
		LastActivity: now,
	}

	var err error
	err = db.RetryOnLock(func() error {
		return db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}},
			DoUpdates: clause.AssignmentColumns(columns),
		}).Create(chat).Error
	})
	if err != nil {
		log.Errorf("[Database] UpdateChat upsert failed for chat %d: %v", chatId, err)
		return err
	}
	defer cache.DeleteCache(cache.CacheKey("chat", chatId))

	// Atomically append userid only if not already present in the JSON array
	err = db.RetryOnLock(func() error {
		return db.DB.Exec(
			`UPDATE chats SET users = json_insert(COALESCE(NULLIF(CAST(users AS TEXT), ''), '[]'), '$[#]', ?) WHERE chat_id = ? AND NOT EXISTS (SELECT 1 FROM json_each(COALESCE(NULLIF(CAST(users AS TEXT), ''), '[]')) WHERE value = ?)`,
			userid, chatId, userid,
		).Error
	})
	if err != nil {
		log.Errorf("[Database] UpdateChat atomic append failed for chat %d user %d: %v", chatId, userid, err)
		return err
	}

	log.Debugf("[Database] UpdateChat: %d", chatId)
	return nil
}

// GetAllChats retrieves all chat records and returns them as a map indexed by chat ID.
// Returns an empty map if an error occurs.
func GetAllChats() map[int64]models.Chat {
	var (
		chatArray []models.Chat
		chatMap   = make(map[int64]models.Chat)
	)
	err := db.DB.Find(&chatArray).Error
	if err != nil {
		log.Errorf("[Database] GetAllChats: %v", err)
		return chatMap
	}

	for _, i := range chatArray {
		chatMap[i.ChatId] = i
	}

	return chatMap
}

// LoadChatStats returns the count of active and inactive chats derived on demand from last_activity.
func LoadChatStats() (activeChats, inactiveChats int) {
	cutoff := time.Now().Add(-GetInactivityThreshold())
	var activeCount, inactiveCount int64

	// Active chats: is_inactive = false AND (last_activity >= cutoff OR last_activity IS NULL OR last_activity = zero)
	err := db.DB.Model(&models.Chat{}).
		Where("is_inactive = ? AND (last_activity >= ? OR last_activity IS NULL OR last_activity = ?)", false, cutoff, time.Time{}).
		Count(&activeCount).Error
	if err != nil {
		log.Errorf("[Database][LoadChatStats] counting active chats: %v", err)
	}

	// Inactive chats: is_inactive = true OR (last_activity < cutoff AND last_activity > zero)
	err = db.DB.Model(&models.Chat{}).
		Where("is_inactive = ? OR (last_activity < ? AND last_activity > ?)", true, cutoff, time.Time{}).
		Count(&inactiveCount).Error
	if err != nil {
		log.Errorf("[Database][LoadChatStats] counting inactive chats: %v", err)
	}

	activeChats = int(activeCount)
	inactiveChats = int(inactiveCount)
	return
}

// LoadActivityStats returns Daily Active Groups, Weekly Active Groups, and Monthly Active Groups.
// These metrics are based on last_activity timestamps within the respective time periods.
func LoadActivityStats() (dag, wag, mag int64) {
	now := time.Now()
	dayAgo := now.Add(-24 * time.Hour)
	weekAgo := now.Add(-7 * 24 * time.Hour)
	monthAgo := now.Add(-30 * 24 * time.Hour)

	// Count daily active groups
	err := db.DB.Model(&models.Chat{}).
		Where("is_inactive = ? AND last_activity >= ?", false, dayAgo).
		Count(&dag).Error
	if err != nil {
		log.Errorf("[Database][LoadActivityStats] counting daily active groups: %v", err)
	}

	// Count weekly active groups
	err = db.DB.Model(&models.Chat{}).
		Where("is_inactive = ? AND last_activity >= ?", false, weekAgo).
		Count(&wag).Error
	if err != nil {
		log.Errorf("[Database][LoadActivityStats] counting weekly active groups: %v", err)
	}

	// Count monthly active groups
	err = db.DB.Model(&models.Chat{}).
		Where("is_inactive = ? AND last_activity >= ?", false, monthAgo).
		Count(&mag).Error
	if err != nil {
		log.Errorf("[Database][LoadActivityStats] counting monthly active groups: %v", err)
	}

	return dag, wag, mag
}
