package blacklists

import (
	"strings"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

// defaultBlacklistAction is used until a chat selects its own action.
// 'warn' is intentional: it is the least destructive option.
const defaultBlacklistAction = "warn"

// defaultBlacklistReason is a format string with a placeholder for the trigger word.
const defaultBlacklistReason = "Blacklisted word: '%s'"

// AddBlacklist adds a new blacklist word to a chat, inheriting the action the
// chat has already selected. The trigger is converted to lowercase before
// storage, and the (chat_id, word) unique constraint makes re-adding an
// existing trigger a no-op instead of a duplicate row.
// Returns an error if the database operation fails.
func AddBlacklist(chatId int64, trigger string) error {
	if err := chats.EnsureChatInDb(chatId, ""); err != nil {
		log.Errorf("[Database] AddBlacklist: %v - %d", err, chatId)
		return err
	}

	blacklist := &models.BlacklistSettings{
		ChatId: chatId,
		Word:   strings.ToLower(trigger),
		Action: currentBlacklistAction(chatId),
		Reason: defaultBlacklistReason,
	}

	err := db.RetryOnLock(func() error {
		return db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}, {Name: "word"}},
			DoNothing: true,
		}).Create(blacklist).Error
	})
	if err != nil {
		log.Errorf("[Database] AddBlacklist: %v - %d", err, chatId)
		return err
	}

	// Invalidate cache after adding blacklist
	cache.DeleteCache(cache.CacheKey("blacklist", chatId))
	return nil
}

// currentBlacklistAction returns the action the chat's existing entries use, or
// the default when the chat has no entries yet.
func currentBlacklistAction(chatId int64) string {
	var action string
	err := db.DB.Model(&models.BlacklistSettings{}).
		Select("action").
		Where("chat_id = ?", chatId).
		Limit(1).
		Scan(&action).Error
	if err != nil || action == "" {
		return defaultBlacklistAction
	}
	return action
}

// RemoveBlacklist removes a specific blacklist word from a chat.
// The trigger is converted to lowercase before removal.
// Returns an error if the database operation fails.
func RemoveBlacklist(chatId int64, trigger string) error {
	var removed int64
	err := db.RetryOnLock(func() error {
		result := db.DB.Where("chat_id = ? AND word = ?", chatId, strings.ToLower(trigger)).Delete(&models.BlacklistSettings{})
		if result.Error != nil {
			return result.Error
		}
		removed = result.RowsAffected
		return nil
	})
	if err != nil {
		log.Errorf("[Database] RemoveBlacklist: %v - %d", err, chatId)
		return err
	}

	// Invalidate cache if something was deleted
	if removed > 0 {
		cache.DeleteCache(cache.CacheKey("blacklist", chatId))
	}
	return nil
}

// RemoveAllBlacklist removes all blacklist entries for a specific chat.
// Returns an error if the database operation fails.
func RemoveAllBlacklist(chatId int64) error {
	err := db.RetryOnLock(func() error {
		return db.DB.Where("chat_id = ?", chatId).Delete(&models.BlacklistSettings{}).Error
	})
	if err != nil {
		log.Errorf("[Database] RemoveAllBlacklist: %v - %d", err, chatId)
		return err
	}

	// Invalidate cache after removing all blacklist entries
	cache.DeleteCache(cache.CacheKey("blacklist", chatId))
	return nil
}

// SetBlacklistAction updates the action for all blacklist entries in a chat.
// The action is converted to lowercase before storage.
func SetBlacklistAction(chatId int64, action string) error {
	err := db.RetryOnLock(func() error {
		return db.DB.Model(&models.BlacklistSettings{}).Where("chat_id = ?", chatId).Update("action", strings.ToLower(action)).Error
	})
	if err != nil {
		log.Errorf("[Database] SetBlacklistAction: %v - %d", err, chatId)
		return err
	}

	// Invalidate cache after updating action
	cache.DeleteCache(cache.CacheKey("blacklist", chatId))
	return nil
}

// GetBlacklistSettings retrieves all blacklist settings for a chat with caching support.
// Returns an empty slice if no blacklists are found or on error.
func GetBlacklistSettings(chatId int64) models.BlacklistSettingsSlice {
	// Try to get from cache first
	cacheKey := cache.CacheKey("blacklist", chatId)
	result, err := cache.GetFromCacheOrLoad(cacheKey, cache.CacheTTLBlacklist, func() (models.BlacklistSettingsSlice, error) {
		var blacklists []*models.BlacklistSettings
		err := db.GetRecords(&blacklists, models.BlacklistSettings{ChatId: chatId})
		if err != nil {
			log.Errorf("[Database] GetBlacklistSettings: %v - %d", err, chatId)
			return models.BlacklistSettingsSlice{}, err
		}
		return models.BlacklistSettingsSlice(blacklists), nil
	})
	if err != nil {
		return models.BlacklistSettingsSlice{}
	}
	return result
}

// LoadBlacklistsStats returns statistics about blacklist usage.
// Returns the total number of blacklist entries and distinct chats using blacklists.
func LoadBlacklistsStats() (blacklistTriggers, blacklistChats int64) {
	// Count total blacklist entries
	err := db.DB.Model(&models.BlacklistSettings{}).Count(&blacklistTriggers).Error
	if err != nil {
		log.Errorf("[Database] LoadBlacklistsStats (triggers): %v", err)
		return 0, 0
	}

	// Count distinct chats with blacklists
	err = db.DB.Model(&models.BlacklistSettings{}).Distinct("chat_id").Count(&blacklistChats).Error
	if err != nil {
		log.Errorf("[Database] LoadBlacklistsStats (chats): %v", err)
		return blacklistTriggers, 0
	}

	return
}
