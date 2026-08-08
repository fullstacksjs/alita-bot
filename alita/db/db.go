package db

import (
	"context"
	"errors"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/divkix/Alita_Robot/alita/db/models"
)

// Re-export model types for backward compatibility
type (
	Button                 = models.Button
	ButtonArray            = models.ButtonArray
	StringArray            = models.StringArray
	Int64Array             = models.Int64Array
	User                   = models.User
	Chat                   = models.Chat
	WarnSettings           = models.WarnSettings
	WarnEvent              = models.WarnEvent
	Warns                  = models.Warns
	GreetingSettings       = models.GreetingSettings
	WelcomeSettings        = models.WelcomeSettings
	ChatFilters            = models.ChatFilters
	BlacklistSettings      = models.BlacklistSettings
	BlacklistSettingsSlice = models.BlacklistSettingsSlice
	ChannelSettings        = models.ChannelSettings
	AntifloodSettings      = models.AntifloodSettings
	ConnectionSettings     = models.ConnectionSettings
	NotesSettings          = models.NotesSettings
	Notes                  = models.Notes
	ApprovedUsers          = models.ApprovedUsers
	AntiRaidSettings       = models.AntiRaidSettings
	Reactions              = models.Reactions
)

// Message type constants - maintain compatibility with existing code
const (
	TEXT       int = 1
	STICKER    int = 2
	DOCUMENT   int = 3
	PHOTO      int = 4
	AUDIO      int = 5
	VOICE      int = 6
	VIDEO      int = 7
	VIDEO_NOTE int = 8
)

// DefaultWelcome is used when no custom welcome is configured.
const DefaultWelcome = "Hey {first}, how are you?"

// CreateRecord creates a new database record using the provided model.
func CreateRecord(model any) error {
	result := DB.WithContext(context.Background()).Create(model)
	if result.Error != nil {
		log.Errorf("[Database][CreateRecord]: %v", result.Error)
		return result.Error
	}
	return nil
}

// UpdateRecord updates an existing database record with the provided updates.
func UpdateRecord(model any, where any, updates any) error {
	return updateRecordInternal(context.Background(), model, where, updates, "UpdateRecord")
}

// UpdateRecordWithZeroValues updates a database record including zero values.
func UpdateRecordWithZeroValues(model any, where any, updates map[string]any) error {
	return updateRecordInternal(context.Background(), model, where, updates, "UpdateRecordWithZeroValues")
}

// updateRecordInternal is the shared implementation for record updates.
func updateRecordInternal(ctx context.Context, model any, where any, updates any, logPrefix string) error {
	result := DB.WithContext(ctx).Model(model).Where(where).Updates(updates)
	if result.Error != nil {
		log.Errorf("[Database][%s]: %v", logPrefix, result.Error)
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetRecord retrieves a single database record matching the where clause.
func GetRecord(model any, where any) error {
	result := DB.WithContext(context.Background()).Where(where).First(model)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			log.Errorf("[Database][GetRecord]: %v", result.Error)
		}
		return result.Error
	}
	return nil
}

// ChatExists checks if a chat with the given ID exists in the database.
func ChatExists(chatID int64) bool {
	chatExists := &Chat{}
	err := GetRecord(chatExists, Chat{ChatId: chatID})
	return !errors.Is(err, gorm.ErrRecordNotFound)
}

// GetRecords retrieves multiple database records matching the where clause.
func GetRecords(models any, where any) error {
	result := DB.WithContext(context.Background()).Where(where).Find(models)
	if result.Error != nil {
		log.Errorf("[Database][GetRecords]: %v", result.Error)
		return result.Error
	}
	return nil
}
