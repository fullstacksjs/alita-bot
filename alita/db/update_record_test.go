package db

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestUpdateRecordReturnsErrorOnNoMatch(t *testing.T) {
	skipIfNoDb(t)

	// Use a chat ID that doesn't exist in the database
	nonExistentChatID := time.Now().UnixNano()

	err := UpdateRecord(
		&AntifloodSettings{},
		AntifloodSettings{ChatId: nonExistentChatID},
		map[string]any{"flood_limit": 5},
	)
	if err == nil {
		t.Fatalf("expected error for non-existent record, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got: %v", err)
	}
}

func TestUpdateRecordWithZeroValuesReturnsErrorOnNoMatch(t *testing.T) {
	skipIfNoDb(t)

	nonExistentChatID := time.Now().UnixNano()

	err := UpdateRecordWithZeroValues(
		&AntifloodSettings{},
		AntifloodSettings{ChatId: nonExistentChatID},
		map[string]any{"flood_limit": 0},
	)
	if err == nil {
		t.Fatalf("expected error for non-existent record, got nil")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got: %v", err)
	}
}

func TestUpdateRecordWithZeroValuesUpdatesZeroValues(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	if err := EnsureChatInDb(chatID, ""); err != nil {
		t.Fatalf("failed to ensure parent chat: %v", err)
	}

	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&AntifloodSettings{}).Error
		_ = DB.Where("chat_id = ?", chatID).Delete(&Chat{}).Error
	})

	// Create a record with Limit=5
	if err := DB.Create(&AntifloodSettings{ChatId: chatID, Limit: 5, Action: "ban"}).Error; err != nil {
		t.Fatalf("failed to create test record: %v", err)
	}

	// Update to Limit=0 using UpdateRecordWithZeroValues
	err := UpdateRecordWithZeroValues(
		&AntifloodSettings{},
		AntifloodSettings{ChatId: chatID},
		map[string]any{"flood_limit": 0},
	)
	if err != nil {
		t.Fatalf("UpdateRecordWithZeroValues() error = %v", err)
	}

	var flood AntifloodSettings
	if err := DB.Where("chat_id = ?", chatID).First(&flood).Error; err != nil {
		t.Fatalf("query error: %v", err)
	}
	if flood.Limit != 0 {
		t.Fatalf("expected Limit=0 after zero-value update, got %d", flood.Limit)
	}
}

func TestUpdateRecordSucceedsWhenRowsAffected(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	if err := EnsureChatInDb(chatID, ""); err != nil {
		t.Fatalf("failed to ensure parent chat: %v", err)
	}

	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&AntifloodSettings{}).Error
		_ = DB.Where("chat_id = ?", chatID).Delete(&Chat{}).Error
	})

	// Create a record
	if err := DB.Create(&AntifloodSettings{ChatId: chatID, Limit: 0, Action: "mute"}).Error; err != nil {
		t.Fatalf("failed to create test record: %v", err)
	}

	// Update it — should succeed (rows affected > 0)
	err := UpdateRecord(
		&AntifloodSettings{},
		AntifloodSettings{ChatId: chatID},
		map[string]any{"flood_limit": 10},
	)
	if err != nil {
		t.Fatalf("UpdateRecord() error = %v, expected nil", err)
	}

	var flood AntifloodSettings
	if err := DB.Where("chat_id = ?", chatID).First(&flood).Error; err != nil {
		t.Fatalf("query error: %v", err)
	}
	if flood.Limit != 10 {
		t.Fatalf("expected Limit=10 after update, got %d", flood.Limit)
	}
}
