package warns

import (
	"errors"
	"unicode/utf8"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/db/user"
	"github.com/divkix/Alita_Robot/alita/i18n"
)

// checkWarnSettings retrieves or creates default warn settings for a chat.
// Returns default settings with warn limit 3 if the chat doesn't exist.
func checkWarnSettings(chatID int64) (warnrc *models.WarnSettings) {
	defaultWarnSettings := &models.WarnSettings{ChatId: chatID, WarnLimit: 3}
	warnrc = &models.WarnSettings{}
	err := db.DB.Where("chat_id = ?", chatID).First(warnrc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !db.ChatExists(chatID) {
			log.Warnf("[Database][checkWarnSettings]: Chat %d doesn't exist, returning default settings", chatID)
			return defaultWarnSettings
		}

		warnrc = defaultWarnSettings
		err := db.DB.Create(warnrc).Error
		if err != nil {
			log.Errorf("[Database] checkWarnSettings: %v", err)
		}
	} else if err != nil {
		log.Errorf("[Database][checkWarnSettings]: %d - %v", chatID, err)
		warnrc = defaultWarnSettings
	}
	return
}

// WarnUser adds a warning event to a user in a specific chat with an optional reason.
// Returns the total number of warnings, all reasons, and any persistence error.
func WarnUser(userId, chatId int64, reason string) (int, []string, error) {
	var numWarns int
	var reasons []string

	if err := chats.EnsureChatInDb(chatId, ""); err != nil {
		return 0, nil, err
	}
	if err := user.EnsureUserInDb(userId, "", ""); err != nil {
		return 0, nil, err
	}

	formattedReason := reason
	if formattedReason != "" {
		if len(formattedReason) > 3000 {
			formattedReason = formattedReason[:3000]
			for !utf8.ValidString(formattedReason) {
				formattedReason = formattedReason[:len(formattedReason)-1]
			}
		}
	} else {
		tr := i18n.English()
		noReason, _ := tr.GetString("db_warn_no_reason")
		if noReason == "" {
			noReason = "No Reason"
		}
		formattedReason = noReason
	}

	err := db.RetryOnLock(func() error {
		return db.DB.Transaction(func(tx *gorm.DB) error {
			warnSettings := &models.WarnSettings{}
			if err := tx.Where("chat_id = ?", chatId).First(warnSettings).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					warnSettings = &models.WarnSettings{ChatId: chatId, WarnLimit: 3}
					if err := tx.Create(warnSettings).Error; err != nil {
						return err
					}
				} else {
					return err
				}
			}

			event := &models.WarnEvent{
				UserId: userId,
				ChatId: chatId,
				Reason: formattedReason,
			}
			if err := tx.Create(event).Error; err != nil {
				return err
			}

			var events []models.WarnEvent
			if err := tx.Where("user_id = ? AND chat_id = ?", userId, chatId).Order("id ASC").Find(&events).Error; err != nil {
				return err
			}

			numWarns = len(events)
			reasons = make([]string, len(events))
			for i, ev := range events {
				reasons[i] = ev.Reason
			}
			return nil
		})
	})
	if err != nil {
		log.Errorf("[Database] WarnUser: %v", err)
		return 0, nil, err
	}

	cache.DeleteCache(cache.CacheKey("warns", userId, chatId))
	cache.DeleteCache(cache.CacheKey("warn_settings", chatId))

	return numWarns, reasons, nil
}

// removeWarnAttempts bounds the retries used to claim a warning event when
// several removals race for the same row.
const removeWarnAttempts = 10

// RemoveWarn removes the most recent warning event from a user in a specific chat.
// Returns whether a warning was removed and any persistence error.
//
// The delete targets the newest event in a single statement rather than reading
// it first and deleting it afterwards: under PostgreSQL's READ COMMITTED, two
// concurrent removals would otherwise both read the same event and both report
// success while only one row disappeared. When another caller claims the chosen
// event first, this retries against the next one so every caller that reports a
// removal really removed a warning.
func RemoveWarn(userId, chatId int64) (bool, error) {
	var removed bool

	err := db.RetryOnLock(func() error {
		removed = false
		for range removeWarnAttempts {
			newest := db.DB.Model(&models.WarnEvent{}).
				Select("id").
				Where("user_id = ? AND chat_id = ?", userId, chatId).
				Order("id DESC").
				Limit(1)

			result := db.DB.Where("id = (?)", newest).Delete(&models.WarnEvent{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				removed = true
				return nil
			}

			// Nothing was deleted: either this user has no warnings left, or a
			// concurrent removal claimed the event first. Only the latter is
			// worth another attempt.
			var remaining int64
			if err := db.DB.Model(&models.WarnEvent{}).
				Where("user_id = ? AND chat_id = ?", userId, chatId).
				Count(&remaining).Error; err != nil {
				return err
			}
			if remaining == 0 {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		log.Errorf("[Database] RemoveWarn: %v", err)
		return false, err
	}

	if removed {
		cache.DeleteCache(cache.CacheKey("warns", userId, chatId))
		cache.DeleteCache(cache.CacheKey("warn_settings", chatId))
	}

	return removed, nil
}

// ResetUserWarns removes all warning events for a specific user in a chat.
// Returns whether any rows were deleted and any persistence error.
func ResetUserWarns(userId, chatId int64) (bool, error) {
	var rowsAffected int64
	err := db.RetryOnLock(func() error {
		res := db.DB.Where("user_id = ? AND chat_id = ?", userId, chatId).Delete(&models.WarnEvent{})
		if res.Error != nil {
			return res.Error
		}
		rowsAffected = res.RowsAffected
		return nil
	})
	if err != nil {
		log.Errorf("[Database] ResetUserWarns: %v", err)
		return false, err
	}
	if rowsAffected == 0 {
		return false, nil
	}
	cache.DeleteCache(cache.CacheKey("warns", userId, chatId))
	cache.DeleteCache(cache.CacheKey("warn_settings", chatId))
	return true, nil
}

// GetWarns retrieves the current warning count and reasons for a user in a specific chat.
// Results are cached to avoid repeated database queries.
func GetWarns(userId, chatId int64) (int, []string) {
	type warnCache struct {
		NumWarns int
		Reasons  []string
	}
	cached, err := cache.GetFromCacheOrLoad(
		cache.CacheKey("warns", userId, chatId),
		cache.CacheTTLWarns,
		func() (warnCache, error) {
			var events []models.WarnEvent
			if err := db.DB.Where("user_id = ? AND chat_id = ?", userId, chatId).Order("id ASC").Find(&events).Error; err != nil {
				return warnCache{}, err
			}
			reasons := make([]string, len(events))
			for i, ev := range events {
				reasons[i] = ev.Reason
			}
			return warnCache{NumWarns: len(events), Reasons: reasons}, nil
		},
	)
	if err != nil {
		var events []models.WarnEvent
		if err := db.DB.Where("user_id = ? AND chat_id = ?", userId, chatId).Order("id ASC").Find(&events).Error; err != nil {
			return 0, nil
		}
		reasons := make([]string, len(events))
		for i, ev := range events {
			reasons[i] = ev.Reason
		}
		return len(events), reasons
	}
	return cached.NumWarns, cached.Reasons
}

// SetWarnLimit updates the warning limit for a specific chat.
func SetWarnLimit(chatId int64, warnLimit int) error {
	warnrc := checkWarnSettings(chatId)
	warnrc.WarnLimit = warnLimit
	err := db.RetryOnLock(func() error {
		return db.DB.Save(warnrc).Error
	})
	if err != nil {
		log.Errorf("[Database] SetWarnLimit: %v", err)
		return err
	}
	cache.DeleteCache(cache.CacheKey("warn_settings", chatId))
	return nil
}

// GetWarnSetting returns the warning settings for the specified chat.
func GetWarnSetting(chatId int64) *models.WarnSettings {
	cached, err := cache.GetFromCacheOrLoad(
		cache.CacheKey("warn_settings", chatId),
		cache.CacheTTLWarns,
		func() (*models.WarnSettings, error) {
			return checkWarnSettings(chatId), nil
		},
	)
	if err != nil {
		return checkWarnSettings(chatId)
	}
	return cached
}

// GetAllChatWarns returns the total count of warned users in a specific chat.
func GetAllChatWarns(chatId int64) int {
	var count int64
	err := db.DB.Model(&models.WarnEvent{}).
		Where("chat_id = ?", chatId).
		Select("COUNT(DISTINCT user_id)").
		Scan(&count).Error
	if err != nil {
		log.Errorf("[Database] GetAllChatWarns: %v", err)
		return 0
	}
	return int(count)
}

// ResetAllChatWarns removes all warning records for all users in a specific chat.
func ResetAllChatWarns(chatId int64) error {
	var userIds []int64
	if err := db.DB.Model(&models.WarnEvent{}).Where("chat_id = ?", chatId).Pluck("user_id", &userIds).Error; err != nil {
		log.Errorf("[Database] ResetAllChatWarns: %v", err)
		return err
	}

	err := db.RetryOnLock(func() error {
		return db.DB.Where("chat_id = ?", chatId).Delete(&models.WarnEvent{}).Error
	})
	if err != nil {
		log.Errorf("[Database] ResetAllChatWarns: %v", err)
		return err
	}
	for _, userId := range userIds {
		cache.DeleteCache(cache.CacheKey("warns", userId, chatId))
	}
	cache.DeleteCache(cache.CacheKey("warn_settings", chatId))
	return nil
}
