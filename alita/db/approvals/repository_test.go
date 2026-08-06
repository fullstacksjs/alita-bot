package approvals

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/migrations"
)

func TestMain(m *testing.M) {
	var dbFileName string
	if db.DB == nil {
		dbFile, err := os.CreateTemp("", "alita_test_*.db")
		if err != nil {
			fmt.Printf("temp file creation failed: %v\n", err)
			os.Exit(1)
		}
		dbFileName = dbFile.Name()
		if closeErr := dbFile.Close(); closeErr != nil {
			fmt.Printf("temp file close failed: %v\n", closeErr)
			os.Exit(1)
		}
		dbPath := db.FormatSQLiteDSN(dbFileName)
		sqliteDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			fmt.Printf("SQLite init failed: %v\n", err)
			os.Exit(1)
		}
		sqliteDB.Exec("PRAGMA foreign_keys = ON;")
		sqliteDB.Exec("PRAGMA journal_mode = WAL;")
		sqliteDB.Exec("PRAGMA busy_timeout = 10000;")

		sqlDB, err := sqliteDB.DB()
		if err != nil {
			fmt.Printf("SQLite handle failed: %v\n", err)
			os.Exit(1)
		}
		sqlDB.SetMaxOpenConns(5)
		sqlDB.SetMaxIdleConns(5)

		runner := migrations.NewSQLiteMigrationRunner(sqliteDB)
		if err := runner.RunMigrations(); err != nil {
			fmt.Printf("Migration failed: %v\n", err)
			os.Exit(1)
		}

		db.DB = sqliteDB
	}

	exitCode := m.Run()

	if db.DB != nil {
		sqlDB, err := db.DB.DB()
		if err != nil {
			fmt.Printf("failed to get underlying DB: %v\n", err)
		} else if closeErr := sqlDB.Close(); closeErr != nil {
			fmt.Printf("DB close failed: %v\n", closeErr)
		}
	}

	if dbFileName != "" {
		if rmErr := os.Remove(dbFileName); rmErr != nil {
			fmt.Printf("temp file remove failed: %v\n", rmErr)
		}
	}

	os.Exit(exitCode)
}

func skipIfNoDb(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
}

func clearApprovalsForTest(chatID int64) {
	for _, approved := range GetApprovedUsers(chatID) {
		_ = RemoveApprovedUser(chatID, approved.UserID)
	}
}

func TestIsUserApproved(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999999)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	// User should not be approved initially
	if IsUserApproved(chatID, 12345) {
		t.Fatalf("IsUserApproved() = true, expected false for non-existent user")
	}

	// Approve user and check
	if err := AddApprovedUser(chatID, 12345, 99999, "trusted member"); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}
	if !IsUserApproved(chatID, 12345) {
		t.Fatalf("IsUserApproved() = false, expected true after approval")
	}

	// Different user should not be approved
	if IsUserApproved(chatID, 99999) {
		t.Fatalf("IsUserApproved() = true for unapproved user")
	}

	// Different chat should not have user approved
	if IsUserApproved(-888888888888, 12345) {
		t.Fatalf("IsUserApproved() = true in wrong chat")
	}
}

func TestAddApprovedUser(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999999)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	if err := AddApprovedUser(chatID, 11111, 99999, "test reason"); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}

	users := GetApprovedUsers(chatID)
	if len(users) != 1 {
		t.Fatalf("GetApprovedUsers() returned %d users, expected 1", len(users))
	}
	if users[0].UserID != 11111 {
		t.Fatalf("expected UserID=11111, got %d", users[0].UserID)
	}
	if users[0].Reason != "test reason" {
		t.Fatalf("expected Reason='test reason', got %q", users[0].Reason)
	}
	if users[0].ApprovedBy != 99999 {
		t.Fatalf("expected ApprovedBy=99999, got %d", users[0].ApprovedBy)
	}
}

func TestRemoveApprovedUser(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999999)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	// Add two users, remove one
	if err := AddApprovedUser(chatID, 100, 1, ""); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}
	if err := AddApprovedUser(chatID, 200, 1, ""); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}

	if err := RemoveApprovedUser(chatID, 100); err != nil {
		t.Fatalf("RemoveApprovedUser() error = %v", err)
	}

	if IsUserApproved(chatID, 100) {
		t.Fatalf("IsUserApproved() = true after removal")
	}
	if !IsUserApproved(chatID, 200) {
		t.Fatalf("IsUserApproved() = false for remaining user")
	}
}

func TestGetApprovedUsers(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999999)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	// Empty chat returns empty slice, not nil
	users := GetApprovedUsers(chatID)
	if users == nil {
		t.Fatalf("GetApprovedUsers() returned nil, expected empty slice")
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 approved users for new chat, got %d", len(users))
	}

	// Add users and verify
	if err := AddApprovedUser(chatID, 10, 1, "reason1"); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}
	if err := AddApprovedUser(chatID, 20, 1, "reason2"); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}

	users = GetApprovedUsers(chatID)
	if len(users) != 2 {
		t.Fatalf("expected 2 approved users, got %d", len(users))
	}
}

func TestCacheInvalidationOnWrite(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999998)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	// Add initial user
	if err := AddApprovedUser(chatID, 5555, 1, ""); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}

	// Populate cache
	users1 := GetApprovedUsers(chatID)
	if len(users1) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users1))
	}

	// Add another user and verify cache invalidated
	if err := AddApprovedUser(chatID, 6666, 1, ""); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}

	users2 := GetApprovedUsers(chatID)
	if len(users2) != 2 {
		t.Fatalf("cache not invalidated: expected 2 users after add, got %d", len(users2))
	}

	// Remove user and verify cache invalidated
	if err := RemoveApprovedUser(chatID, 5555); err != nil {
		t.Fatalf("RemoveApprovedUser() error = %v", err)
	}

	users3 := GetApprovedUsers(chatID)
	if len(users3) != 1 {
		t.Fatalf("cache not invalidated: expected 1 user after remove, got %d", len(users3))
	}
}

func TestDuplicateApprovalUpdatesExemption(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999997)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	if err := AddApprovedUser(chatID, 7777, 1, "initial reason"); err != nil {
		t.Fatalf("AddApprovedUser() error = %v", err)
	}

	// Re-approving user updates reason and audit fields idempotently without duplicate row
	if err := AddApprovedUser(chatID, 7777, 2, "updated reason"); err != nil {
		t.Fatalf("AddApprovedUser() update error = %v", err)
	}

	users := GetApprovedUsers(chatID)
	if len(users) != 1 {
		t.Fatalf("expected 1 approved user, got %d", len(users))
	}
	if users[0].Reason != "updated reason" {
		t.Fatalf("expected Reason='updated reason', got %q", users[0].Reason)
	}
	if users[0].ApprovedBy != 2 {
		t.Fatalf("expected ApprovedBy=2, got %d", users[0].ApprovedBy)
	}
}

func TestConcurrentApprovals(t *testing.T) {
	skipIfNoDb(t)

	chatID := int64(-999999999999996)
	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers)

	t.Cleanup(func() {
		clearApprovalsForTest(chatID)
	})

	for i := 0; i < workers; i++ {
		userID := int64(1000 + i)
		go func(u int64) {
			defer wg.Done()
			if err := AddApprovedUser(chatID, u, 1, "concurrent"); err != nil {
				t.Errorf("AddApprovedUser(%d) error = %v", u, err)
			}
			if !IsUserApproved(chatID, u) {
				t.Errorf("IsUserApproved(%d) = false, want true", u)
			}
		}(userID)
	}

	wg.Wait()

	users := GetApprovedUsers(chatID)
	if len(users) != workers {
		t.Fatalf("GetApprovedUsers() returned %d users, want %d", len(users), workers)
	}
}
