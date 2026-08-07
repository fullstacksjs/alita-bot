package antiraid

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
)

// MaxRaidDuration bounds how long a raid window may stay open (366 days).
const MaxRaidDuration = 366 * 24 * 60 * 60

// RaidState describes the active-raid window of a chat. Timestamps are unix
// seconds and are zero when no raid is active.
type RaidState struct {
	Active    bool
	StartedAt int64
	ExpiresAt int64
}

// defaultAntiRaidSettings returns default settings for a chat when no record exists.
// Raid time: 6h (21600s), action time: 1h (3600s), auto threshold: 0 (disabled).
func defaultAntiRaidSettings(chatID int64) *models.AntiRaidSettings {
	return &models.AntiRaidSettings{
		ChatID:                chatID,
		RaidTime:              21600,
		RaidActionTime:        3600,
		AutoAntiRaidThreshold: 0,
	}
}

// GetAntiRaidSettings retrieves anti-raid settings for a chat.
// Returns defaults if no record exists.
func GetAntiRaidSettings(chatID int64) *models.AntiRaidSettings {
	settings, err := GetAntiRaidSettingsCached(chatID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return defaultAntiRaidSettings(chatID)
		}
		log.Errorf("[Database][GetAntiRaidSettings]: %v", err)
		return defaultAntiRaidSettings(chatID)
	}
	return settings
}

// upsertChatField upserts the given column updates for a chat's anti-raid settings
// and invalidates the antiraid cache. Callers handle any validation guards.
//
// The write is a single conflict-safe upsert on chat_id, so concurrent setting
// changes cannot lose updates through a read-then-create race and the
// active-raid window is never disturbed by a configuration change.
func upsertChatField(chatID int64, updates map[string]any) error {
	if err := chats.EnsureChatInDb(chatID, ""); err != nil {
		log.Errorf("[Database] upsertChatField: %v - %d", err, chatID)
		return err
	}

	defaults := defaultAntiRaidSettings(chatID)
	now := time.Now()
	row := map[string]any{
		"chat_id":                 chatID,
		"raid_time":               defaults.RaidTime,
		"raid_action_time":        defaults.RaidActionTime,
		"auto_antiraid_threshold": defaults.AutoAntiRaidThreshold,
		"created_at":              now,
		"updated_at":              now,
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
		return db.DB.Model(&models.AntiRaidSettings{}).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}},
			DoUpdates: clause.AssignmentColumns(assigned),
		}).Create(row).Error
	})
	if err != nil {
		log.Errorf("[Database] upsertChatField: %v - %d", err, chatID)
		return err
	}

	cache.DeleteCache(cache.CacheKey("antiraid", chatID))
	return nil
}

// SetRaidTime sets the raid duration (in seconds) for a chat.
func SetRaidTime(chatID int64, seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("raid time must be non-negative, got %d", seconds)
	}
	if int64(seconds) > math.MaxInt32 {
		return fmt.Errorf("raid time exceeds a PostgreSQL integer, got %d", seconds)
	}

	updates := map[string]any{
		"chat_id":   chatID,
		"raid_time": seconds,
	}
	return upsertChatField(chatID, updates)
}

// SetRaidActionTime sets the ban/restriction duration during a raid (in seconds).
func SetRaidActionTime(chatID int64, seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("raid action time must be non-negative, got %d", seconds)
	}
	if int64(seconds) > math.MaxInt32 {
		return fmt.Errorf("raid action time exceeds a PostgreSQL integer, got %d", seconds)
	}

	updates := map[string]any{
		"chat_id":          chatID,
		"raid_action_time": seconds,
	}
	return upsertChatField(chatID, updates)
}

// SetAutoAntiRaidThreshold sets the auto-trigger join-rate threshold.
// 0 disables auto-trigger.
func SetAutoAntiRaidThreshold(chatID int64, threshold int) error {
	if threshold < 0 {
		return fmt.Errorf("threshold must be non-negative, got %d", threshold)
	}

	updates := map[string]any{
		"chat_id":                 chatID,
		"auto_antiraid_threshold": threshold,
	}
	return upsertChatField(chatID, updates)
}

// GetRaidState returns the persisted active-raid window for a chat. A stored
// window whose deadline has already passed reads as inactive; the row itself is
// cleared by the expiry worker or by the next state change.
func GetRaidState(chatID int64) *RaidState {
	if db.DB == nil {
		return &RaidState{}
	}

	var settings models.AntiRaidSettings
	err := db.DB.Model(&models.AntiRaidSettings{}).
		Select("id, chat_id, raid_started_at, raid_active_until").
		Where("chat_id = ?", chatID).
		First(&settings).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorf("[Database] GetRaidState: %v - %d", err, chatID)
		}
		return &RaidState{}
	}
	return raidStateFrom(&settings)
}

// raidStateFrom converts a settings row into the runtime raid state.
func raidStateFrom(settings *models.AntiRaidSettings) *RaidState {
	if settings.RaidActiveUntil == nil {
		return &RaidState{}
	}
	state := &RaidState{
		ExpiresAt: settings.RaidActiveUntil.Unix(),
		Active:    settings.RaidActiveUntil.After(time.Now()),
	}
	if settings.RaidStartedAt != nil {
		state.StartedAt = settings.RaidStartedAt.Unix()
	}
	return state
}

// EnableRaid opens a raid window of durationSeconds for a chat. It reports
// false without error when a raid is already active, so exactly one concurrent
// caller wins and the existing deadline is preserved.
func EnableRaid(chatID int64, durationSeconds int) (bool, error) {
	if durationSeconds <= 0 || durationSeconds > MaxRaidDuration {
		return false, fmt.Errorf("duration must be between 1 second and 366 days")
	}
	if err := ensureSettingsRow(chatID); err != nil {
		return false, err
	}

	now := time.Now()
	until := now.Add(time.Duration(durationSeconds) * time.Second)

	var enabled bool
	err := db.RetryOnLock(func() error {
		result := db.DB.Model(&models.AntiRaidSettings{}).
			Where("chat_id = ? AND (raid_active_until IS NULL OR raid_active_until <= ?)", chatID, now).
			Updates(map[string]any{
				"raid_started_at":   now,
				"raid_active_until": until,
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		enabled = result.RowsAffected > 0
		return nil
	})
	if err != nil {
		log.Errorf("[Database] EnableRaid: %v - %d", err, chatID)
		return false, err
	}
	return enabled, nil
}

// DisableRaid closes an active raid window. It reports false without error when
// no raid was active.
func DisableRaid(chatID int64) (bool, error) {
	if db.DB == nil {
		return false, errors.New("database not initialized")
	}

	now := time.Now()
	var disabled bool
	err := db.RetryOnLock(func() error {
		result := db.DB.Model(&models.AntiRaidSettings{}).
			Where("chat_id = ? AND raid_active_until IS NOT NULL AND raid_active_until > ?", chatID, now).
			Updates(map[string]any{
				"raid_started_at":   nil,
				"raid_active_until": nil,
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		disabled = result.RowsAffected > 0
		return nil
	})
	if err != nil {
		log.Errorf("[Database] DisableRaid: %v - %d", err, chatID)
		return false, err
	}
	return disabled, nil
}

// SetRaidDuration moves the deadline of an active raid, or opens a new window
// when none is active. The whole decision is one conditional statement, so
// concurrent callers cannot resurrect a raid that another caller just closed.
func SetRaidDuration(chatID int64, durationSeconds int) error {
	if durationSeconds <= 0 || durationSeconds > MaxRaidDuration {
		return fmt.Errorf("duration must be between 1 second and 366 days")
	}
	if err := ensureSettingsRow(chatID); err != nil {
		return err
	}

	now := time.Now()
	until := now.Add(time.Duration(durationSeconds) * time.Second)

	return db.RetryOnLock(func() error {
		result := db.DB.Model(&models.AntiRaidSettings{}).
			Where("chat_id = ? AND raid_active_until IS NOT NULL AND raid_active_until > ?", chatID, now).
			Updates(map[string]any{
				"raid_active_until": until,
				"updated_at":        now,
			})
		if result.Error != nil {
			log.Errorf("[Database] SetRaidDuration: %v - %d", result.Error, chatID)
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}

		result = db.DB.Model(&models.AntiRaidSettings{}).
			Where("chat_id = ? AND (raid_active_until IS NULL OR raid_active_until <= ?)", chatID, now).
			Updates(map[string]any{
				"raid_started_at":   now,
				"raid_active_until": until,
				"updated_at":        now,
			})
		if result.Error != nil {
			log.Errorf("[Database] SetRaidDuration: %v - %d", result.Error, chatID)
		}
		return result.Error
	})
}

// ExpireRaids clears every raid window whose deadline has passed and returns the
// chats that were released. Because the window lives in SQLite, the expiry
// worker recovers raids that were opened before the last restart.
func ExpireRaids(now time.Time) ([]int64, error) {
	if db.DB == nil {
		return nil, errors.New("database not initialized")
	}

	var expired []int64
	err := db.RetryOnLock(func() error {
		expired = nil
		var rows []models.AntiRaidSettings
		if err := db.DB.Model(&models.AntiRaidSettings{}).
			Select("id, chat_id").
			Where("raid_active_until IS NOT NULL AND raid_active_until <= ?", now).
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			result := db.DB.Model(&models.AntiRaidSettings{}).
				Where("id = ? AND raid_active_until IS NOT NULL AND raid_active_until <= ?", row.ID, now).
				Updates(map[string]any{
					"raid_started_at":   nil,
					"raid_active_until": nil,
					"updated_at":        time.Now(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				expired = append(expired, row.ChatID)
			}
		}
		return nil
	})
	if err != nil {
		log.Errorf("[Database] ExpireRaids: %v", err)
		return nil, err
	}
	return expired, nil
}

// ensureSettingsRow creates the anti-raid row for a chat when it is missing,
// without disturbing an existing configuration.
func ensureSettingsRow(chatID int64) error {
	if db.DB == nil {
		return errors.New("database not initialized")
	}
	if err := chats.EnsureChatInDb(chatID, ""); err != nil {
		return err
	}
	return db.RetryOnLock(func() error {
		return db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}},
			DoNothing: true,
		}).Create(defaultAntiRaidSettings(chatID)).Error
	})
}
