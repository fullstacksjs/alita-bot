package db_test

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/channels"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/migrations"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/db/stats"
	"github.com/divkix/Alita_Robot/alita/db/user"
)

func setupRealSQLiteTestDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "identity_test.db")
	dsn := db.FormatSQLiteDSN(dbPath)

	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "failed to open SQLite test DB")

	testDB.Exec("PRAGMA foreign_keys = ON;")
	testDB.Exec("PRAGMA journal_mode = WAL;")
	testDB.Exec("PRAGMA busy_timeout = 10000;")

	sqlDB, err := testDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)

	runner := migrations.NewSQLiteMigrationRunner(testDB)
	err = runner.RunMigrations()
	require.NoError(t, err, "embedded migrations failed")

	_ = testDB.AutoMigrate(
		&models.PinSettings{},
		&models.RulesSettings{},
		&models.DisableSettings{},
		&models.DisableChatSettings{},
	)

	origDB := db.DB
	db.DB = testDB

	t.Cleanup(func() {
		db.DB = origDB
		_ = sqlDB.Close()
	})

	return testDB, dbPath
}

func TestSQLiteIdentityPersistenceAndReopen(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "persistence_test.db")
	dsn := db.FormatSQLiteDSN(dbPath)

	// Step 1: Initialize DB and write records
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	testDB.Exec("PRAGMA foreign_keys = ON;")
	testDB.Exec("PRAGMA journal_mode = WAL;")
	testDB.Exec("PRAGMA busy_timeout = 10000;")

	sqlDB, err := testDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(5)

	runner := migrations.NewSQLiteMigrationRunner(testDB)
	require.NoError(t, runner.RunMigrations())

	origDB := db.DB
	db.DB = testDB

	const (
		testUserID    = int64(100001)
		testChatID    = int64(-100999)
		testChannelID = int64(-100888)
	)

	require.NoError(t, user.UpdateUser(testUserID, "testuser", "Test User"))
	require.NoError(t, chats.UpdateChat(testChatID, "Test Group", testUserID))
	require.NoError(t, chats.EnsureChatInDb(testChannelID, "Test Channel"))
	require.NoError(t, channels.UpdateChannel(testChannelID, "Test Channel", "testchan"))

	// Close the DB connection to simulate restart
	require.NoError(t, sqlDB.Close())

	// Step 2: Reopen SQLite DB file
	reopenDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	reopenDB.Exec("PRAGMA foreign_keys = ON;")
	reopenDB.Exec("PRAGMA journal_mode = WAL;")
	reopenDB.Exec("PRAGMA busy_timeout = 10000;")

	reopenSQLDB, err := reopenDB.DB()
	require.NoError(t, err)
	reopenSQLDB.SetMaxOpenConns(5)
	defer reopenSQLDB.Close()

	db.DB = reopenDB
	defer func() { db.DB = origDB }()

	// Verify persistence after reopen
	u, err := user.GetUserBasicInfo(testUserID)
	require.NoError(t, err)
	assert.Equal(t, "testuser", u.UserName)
	assert.Equal(t, "Test User", u.Name)

	c := chats.GetChatSettings(testChatID)
	assert.Equal(t, "Test Group", c.ChatName)
	assert.True(t, slices.Contains(c.Users, testUserID), "memberships must persist in SQLite and survive reopen")

	ch := channels.GetChannelSettings(testChannelID)
	require.NotNil(t, ch)
	assert.Equal(t, "Test Channel", ch.ChannelName)
	assert.Equal(t, "testchan", ch.Username)
}

func TestNoBotTableOrBotScopedKeyInIdentityModel(t *testing.T) {
	database, _ := setupRealSQLiteTestDB(t)

	fakeBot := &gotgbot.Bot{
		User: gotgbot.User{
			Id:        99999,
			IsBot:     true,
			FirstName: "AlitaBot",
			Username:  "alitabot",
		},
	}

	require.NoError(t, user.EnsureBotInDb(fakeBot))

	// Verify bot is stored as an ordinary user in users table
	u, err := user.GetUserBasicInfo(99999)
	require.NoError(t, err)
	assert.Equal(t, "alitabot", u.UserName)
	assert.Equal(t, "AlitaBot", u.Name)

	// Verify database tables: no 'bot' table exists
	var tableNames []string
	require.NoError(t, database.Raw("SELECT name FROM sqlite_master WHERE type='table'").Scan(&tableNames).Error)
	assert.False(t, slices.Contains(tableNames, "bots"), "no bot table in new identity model")
	assert.False(t, slices.Contains(tableNames, "bot"), "no bot table in new identity model")
}

func TestConcurrentTrackingUnderSQLite(t *testing.T) {
	setupRealSQLiteTestDB(t)

	const (
		chatID          = int64(-100777)
		numWorkers      = 20
		numOpsPerWorker = 5
	)

	var wg sync.WaitGroup
	errChan := make(chan error, numWorkers*numOpsPerWorker)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOpsPerWorker; j++ {
				uID := int64(1000 + workerID*10 + j)
				uName := "user_" + string(rune('a'+workerID))
				if err := user.UpdateUser(uID, uName, "Name"); err != nil {
					errChan <- err
				}
				if err := chats.UpdateChat(chatID, "Concurrent Group", uID); err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Fatalf("concurrent tracking error: %v", err)
	}

	// Verify all concurrent memberships are preserved
	chat := chats.GetChatSettings(chatID)
	assert.Equal(t, "Concurrent Group", chat.ChatName)
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < numOpsPerWorker; j++ {
			uID := int64(1000 + i*10 + j)
			assert.True(t, slices.Contains(chat.Users, uID), "membership %d must not be lost during concurrent tracking", uID)
		}
	}
}

func TestActiveAndInactiveChatResultsDerivedFromLastActivity(t *testing.T) {
	setupRealSQLiteTestDB(t)

	now := time.Now()
	recentChatID := int64(-100101)
	oldChatID := int64(-100102)
	inactiveChatID := int64(-100103)

	// Create recent active chat
	require.NoError(t, chats.UpdateChat(recentChatID, "Recent Chat", 101))

	// Create old chat (>30 days inactive)
	require.NoError(t, db.DB.Create(&models.Chat{
		ChatId:       oldChatID,
		ChatName:     "Old Chat",
		LastActivity: now.Add(-40 * 24 * time.Hour),
		IsInactive:   false,
	}).Error)

	// Create explicitly inactive chat
	require.NoError(t, db.DB.Create(&models.Chat{
		ChatId:       inactiveChatID,
		ChatName:     "Explicit Inactive",
		LastActivity: now.Add(-1 * time.Hour),
		IsInactive:   true,
	}).Error)

	recentChat := chats.GetChatSettings(recentChatID)
	oldChat := chats.GetChatSettings(oldChatID)
	inactiveChat := chats.GetChatSettings(inactiveChatID)

	assert.True(t, chats.IsChatActive(recentChat), "recent chat must be active")
	assert.False(t, chats.IsChatActive(oldChat), "old chat (>30d) must be derived as inactive")
	assert.False(t, chats.IsChatActive(inactiveChat), "explicitly inactive chat must be derived as inactive")

	activeCount, inactiveCount := chats.LoadChatStats()
	assert.Equal(t, 1, activeCount, "derived active chats count")
	assert.Equal(t, 2, inactiveCount, "derived inactive chats count")

	allStats := stats.LoadAllStats()
	assert.Contains(t, allStats, "1 active Chats (2 Inactive, 3 Total)")
}
