package approvals

import (
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/db/user"
)

// AddApprovedUser adds a user to the approved list for a chat.
// Approved users are immune from anti-spam measures.
func AddApprovedUser(chatID, userID, approvedBy int64, reason string) error {
	if err := chats.EnsureChatInDb(chatID, ""); err != nil {
		return err
	}
	if err := user.EnsureUserInDb(userID, "", ""); err != nil {
		return err
	}
	if approvedBy != 0 {
		_ = user.EnsureUserInDb(approvedBy, "", "")
	}

	approval := &models.ApprovedUsers{
		ChatID:     chatID,
		UserID:     userID,
		ApprovedBy: approvedBy,
		Reason:     reason,
	}

	err := db.RetryOnLock(func() error {
		return db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chat_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"approved_by", "reason", "updated_at"}),
		}).Create(approval).Error
	})
	if err != nil {
		log.Errorf("[Database] AddApprovedUser: %v - chat:%d user:%d", err, chatID, userID)
		return err
	}

	cache.DeleteCache(cache.CacheKey("approvals", chatID))
	return nil
}

// IsUserApproved checks if a user is in the approved list for a chat.
func IsUserApproved(chatID, userID int64) bool {
	for _, u := range GetApprovedUsers(chatID) {
		if u.UserID == userID {
			return true
		}
	}
	return false
}

// GetApprovedUsers returns all approved users for a chat.
func GetApprovedUsers(chatID int64) []*models.ApprovedUsers {
	cacheKey := cache.CacheKey("approvals", chatID)
	result, err := cache.GetFromCacheOrLoad(cacheKey, cache.CacheTTLApprovals, func() ([]*models.ApprovedUsers, error) {
		var users []*models.ApprovedUsers
		err := db.DB.Where("chat_id = ?", chatID).Find(&users).Error
		if err != nil {
			log.Errorf("[Database] GetApprovedUsers: %v - chat:%d", err, chatID)
			return nil, err
		}
		return users, nil
	})
	if err != nil {
		return nil
	}
	return result
}

// RemoveApprovedUser removes a user from the approved list for a chat.
func RemoveApprovedUser(chatID, userID int64) error {
	err := db.RetryOnLock(func() error {
		return db.DB.Where("chat_id = ? AND user_id = ?", chatID, userID).Delete(&models.ApprovedUsers{}).Error
	})
	if err != nil {
		log.Errorf("[Database] RemoveApprovedUser: %v - chat:%d user:%d", err, chatID, userID)
		return err
	}

	cache.DeleteCache(cache.CacheKey("approvals", chatID))
	return nil
}
