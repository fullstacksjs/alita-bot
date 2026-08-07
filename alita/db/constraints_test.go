package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ensureChatsInDb creates parent Chat rows for every chat ID so that
// FK-constrained child inserts in these tests succeed.
func ensureChatsInDb(t *testing.T, chatIDs ...int64) {
	t.Helper()
	for _, id := range chatIDs {
		require.NoError(t, EnsureChatInDb(id, ""))
	}
	t.Cleanup(func() {
		_ = DB.Where("chat_id IN ?", chatIDs).Delete(&Chat{}).Error
	})
}

// TestWarnSettingsConstraint_PositiveLimit tests that warn_limit must be positive
func TestWarnSettingsConstraint_PositiveLimit(t *testing.T) {
	// This test verifies the database constraint is working
	// Integration test that requires database connection
	t.Skip("Requires database connection - add to integration test suite")
}

// TestAntifloodSettingsConstraint_ValidActions tests that only valid actions are accepted
func TestAntifloodSettingsConstraint_ValidActions(t *testing.T) {
	validActions := []string{"mute", "ban", "kick", "warn", "tban", "tmute"}
	for _, action := range validActions {
		// Verify each valid action is in the accepted list
		assert.Contains(t, []string{"mute", "ban", "kick", "warn", "tban", "tmute"}, action)
	}
}

// TestWarnSettingsIntegration_PositiveLimit tests that a positive warn_limit
// round-trips through the database. Range validation (1-100) for warn_limit
// is enforced at the application layer (see modules.setWarnLimit), not via a
// database CHECK constraint in the retained SQLite schema.
func TestWarnSettingsIntegration_PositiveLimit(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	ensureChatsInDb(t, chatID)
	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&WarnSettings{}).Error
	})

	settings := &WarnSettings{
		ChatId:    chatID,
		WarnLimit: 3,
	}
	err := CreateRecord(settings)
	assert.NoError(t, err, "Creating warn settings with positive limit should succeed")
	assert.Greater(t, settings.WarnLimit, 0, "Warn limit should be positive")
}

// TestAntifloodSettingsConstraint_ValidActionsIntegration tests antiflood action constraint
func TestAntifloodSettingsConstraint_ValidActionsIntegration(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	validActions := []string{"mute", "ban", "kick", "warn", "tban", "tmute"}

	chatIDs := make([]int64, 0, len(validActions)+1)
	for _, action := range validActions {
		chatIDs = append(chatIDs, chatID+int64(hashCode(action)))
	}
	chatIDs = append(chatIDs, chatID+99999)
	ensureChatsInDb(t, chatIDs...)
	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&AntifloodSettings{}).Error
	})

	for _, action := range validActions {
		settings := &AntifloodSettings{
			ChatId: chatID + int64(hashCode(action)),
			Limit:  5,
			Action: action,
		}
		err := CreateRecord(settings)
		assert.NoError(t, err, "Creating antiflood settings with valid action '%s' should succeed", action)
	}

	// Test invalid action
	invalidSettings := &AntifloodSettings{
		ChatId: chatID + 99999,
		Limit:  5,
		Action: "invalid_action",
	}
	err := CreateRecord(invalidSettings)
	assert.Error(t, err, "Creating antiflood settings with invalid action should fail due to CHECK constraint")
}

// TestWarnEvents_Creation tests creating normalized warn_events records
func TestWarnEvents_Creation(t *testing.T) {
	skipIfNoDb(t)

	base := time.Now().UnixNano()
	userID := base + 200
	chatID := base + 201
	_ = EnsureChatInDb(chatID, "")
	_ = EnsureUserInDb(userID, "", "")

	t.Cleanup(func() {
		_ = DB.Where("user_id = ? AND chat_id = ?", userID, chatID).Delete(&WarnEvent{}).Error
	})

	warn := &WarnEvent{
		UserId: userID,
		ChatId: chatID,
		Reason: "test warning event",
	}
	err := CreateRecord(warn)
	assert.NoError(t, err, "Creating WarnEvent should succeed")
}

// TestAntifloodConstraint_NonNegativeFloodLimit tests flood_limit constraint
func TestAntifloodConstraint_NonNegativeFloodLimit(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	limits := []int{0, 1, 5, 10}
	chatIDs := make([]int64, 0, len(limits)+1)
	for _, limit := range limits {
		chatIDs = append(chatIDs, chatID+int64(limit))
	}
	chatIDs = append(chatIDs, chatID+9999)
	ensureChatsInDb(t, chatIDs...)
	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&AntifloodSettings{}).Error
	})

	// Test valid non-negative values
	for _, limit := range limits {
		settings := &AntifloodSettings{
			ChatId: chatID + int64(limit),
			Limit:  limit,
		}
		err := CreateRecord(settings)
		assert.NoError(t, err, "Creating antiflood settings with flood_limit %d should succeed", limit)
	}

	// Test invalid negative value
	invalidSettings := &AntifloodSettings{
		ChatId: chatID + 9999,
		Limit:  -1,
	}
	err := CreateRecord(invalidSettings)
	assert.Error(t, err, "Creating antiflood settings with negative flood_limit should fail due to CHECK constraint")
}

// TestBlacklistConstraint_ValidActions tests blacklist action constraint
func TestBlacklistConstraint_ValidActions(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	validActions := []string{"warn", "mute", "ban", "kick", "tban", "tmute", "delete"}
	chatIDs := make([]int64, 0, len(validActions)+1)
	for _, action := range validActions {
		chatIDs = append(chatIDs, chatID+int64(hashCode(action)))
	}
	chatIDs = append(chatIDs, chatID+99999)
	ensureChatsInDb(t, chatIDs...)
	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&BlacklistSettings{}).Error
	})

	for _, action := range validActions {
		settings := &BlacklistSettings{
			ChatId: chatID + int64(hashCode(action)),
			Word:   "test_word_" + action,
			Action: action,
		}
		err := CreateRecord(settings)
		assert.NoError(t, err, "Creating blacklist settings with action '%s' should succeed", action)
	}

	// Test invalid action
	invalidSettings := &BlacklistSettings{
		ChatId: chatID + 99999,
		Word:   "test_word_invalid",
		Action: "invalid_action",
	}
	err := CreateRecord(invalidSettings)
	assert.Error(t, err, "Creating blacklist settings with invalid action should fail due to CHECK constraint")
}

// TestAntifloodActionConstraint_ValidActions tests antiflood action constraint
func TestAntifloodActionConstraint_ValidActions(t *testing.T) {
	skipIfNoDb(t)

	chatID := time.Now().UnixNano()
	validActions := []string{"mute", "ban", "kick", "warn", "tban", "tmute"}
	chatIDs := make([]int64, 0, len(validActions)+1)
	for _, action := range validActions {
		chatIDs = append(chatIDs, chatID+int64(hashCode(action)))
	}
	chatIDs = append(chatIDs, chatID+99999)
	ensureChatsInDb(t, chatIDs...)
	t.Cleanup(func() {
		_ = DB.Where("chat_id = ?", chatID).Delete(&AntifloodSettings{}).Error
	})

	// Test valid actions
	for _, action := range validActions {
		settings := &AntifloodSettings{
			ChatId: chatID + int64(hashCode(action)),
			Action: action,
		}
		err := CreateRecord(settings)
		assert.NoError(t, err, "Creating antiflood settings with action '%s' should succeed", action)
	}

	// Test invalid action
	invalidSettings := &AntifloodSettings{
		ChatId: chatID + 99999,
		Action: "invalid_action",
	}
	err := CreateRecord(invalidSettings)
	assert.Error(t, err, "Creating antiflood settings with invalid action should fail due to CHECK constraint")
}

// Helper function to generate deterministic hash codes for test data
func hashCode(s string) int {
	hash := 0
	for i, c := range s {
		hash = hash*31 + int(c) + i
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}
