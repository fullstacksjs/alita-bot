package connections

import (
	"errors"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/db/user"
)

func retryOnLock(fn func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if db.DB != nil && db.DB.Dialector.Name() == "sqlite" && strings.Contains(err.Error(), "locked") {
			time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
			continue
		}
		break
	}
	return err
}

// getUserConnectionSetting retrieves connection settings for a user.
// Returns default settings (not connected) if not found, without creating a record.
func getUserConnectionSetting(userID int64) (connectionSrc *models.ConnectionSettings) {
	connectionSrc = &models.ConnectionSettings{}
	err := db.DB.Where("user_id = ?", userID).First(connectionSrc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		connectionSrc = &models.ConnectionSettings{UserId: userID, Connected: false}
	} else if err != nil {
		connectionSrc = &models.ConnectionSettings{UserId: userID, Connected: false}
		log.Errorf("[Database] getUserConnectionSetting: %d - %v", userID, err)
	}

	return connectionSrc
}

// Connection returns the connection settings for a user.
func Connection(UserID int64) *models.ConnectionSettings {
	return getUserConnectionSetting(UserID)
}

// ConnectId connects a user to a specific chat.
// Sets the user's connection status to true and associates them with the chat.
// The user_id uniqueness constraint makes this a single atomic write.
func ConnectId(UserID, chatID int64) error {
	if chatID == 0 {
		err := fmt.Errorf("invalid chat ID %d", chatID)
		log.WithField("userID", UserID).Warningf("[Database] ConnectId: %v", err)
		return err
	}
	if err := chats.EnsureChatInDb(chatID, ""); err != nil {
		return err
	}
	if err := user.EnsureUserInDb(UserID, "", ""); err != nil {
		return err
	}

	connection := &models.ConnectionSettings{UserId: UserID, ChatId: chatID, Connected: true}
	err := retryOnLock(func() error {
		return db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"chat_id", "connected", "updated_at"}),
		}).Create(connection).Error
	})
	if err != nil {
		log.Errorf("[Database] ConnectId: %v - %d", err, chatID)
	}
	return err
}

// DisconnectId disconnects a user from their current chat connection.
// The connection row is removed so no chat history is retained for reconnect.
func DisconnectId(UserID int64) error {
	err := retryOnLock(func() error {
		return db.DB.Where("user_id = ?", UserID).Delete(&models.ConnectionSettings{}).Error
	})
	if err != nil {
		log.Errorf("[Database] DisconnectId: %v - %d", err, UserID)
	}
	return err
}

// LoadConnectionStats returns statistics about connection usage.
// Returns the count of connected users and distinct chats they are connected to.
func LoadConnectionStats() (connectedUsers, connectedChats int64) {
	err := db.DB.Model(&models.ConnectionSettings{}).Where("connected = ?", true).Count(&connectedUsers).Error
	if err != nil {
		log.Error(err)
		return
	}

	err = db.DB.Model(&models.ConnectionSettings{}).
		Where("connected = ?", true).
		Distinct("chat_id").
		Count(&connectedChats).Error
	if err != nil {
		log.Error(err)
		return
	}

	return
}
