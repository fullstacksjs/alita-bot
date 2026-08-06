package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/divkix/Alita_Robot/alita/db/migrations"
)

func createTempSQLiteDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sqlite.db")
	dsn := formatSQLiteDSN(dbPath)

	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "failed to open SQLite database")

	database.Exec("PRAGMA foreign_keys = ON;")
	database.Exec("PRAGMA journal_mode = WAL;")
	database.Exec("PRAGMA busy_timeout = 10000;")

	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)

	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return database, dbPath
}

func TestSQLiteBootstrap_EmptyFileInitializedFromEmbeddedMigrations(t *testing.T) {
	database, _ := createTempSQLiteDB(t)

	runner := migrations.NewSQLiteMigrationRunner(database)
	err := runner.RunMigrations()
	require.NoError(t, err, "embedded migrations should apply cleanly to a new empty SQLite file")

	var count int64
	err = database.Model(&migrations.SchemaMigration{}).Count(&count).Error
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1), "migration versions should be recorded in schema_migrations")

	var record migrations.SchemaMigration
	err = database.Where("version LIKE ?", "%sqlite_baseline%").First(&record).Error
	require.NoError(t, err, "sqlite baseline migration record must exist")
	assert.NotEmpty(t, record.Checksum, "checksum should be calculated and stored")
}

func TestSQLiteBootstrap_SecondStartupAppliesNothingTwice(t *testing.T) {
	database, _ := createTempSQLiteDB(t)

	runner := migrations.NewSQLiteMigrationRunner(database)
	err := runner.RunMigrations()
	require.NoError(t, err)

	var firstCount int64
	err = database.Model(&migrations.SchemaMigration{}).Count(&firstCount).Error
	require.NoError(t, err)

	// Second startup run
	runner2 := migrations.NewSQLiteMigrationRunner(database)
	err = runner2.RunMigrations()
	require.NoError(t, err, "second run should succeed without error")

	var secondCount int64
	err = database.Model(&migrations.SchemaMigration{}).Count(&secondCount).Error
	require.NoError(t, err)

	assert.Equal(t, firstCount, secondCount, "second startup must not apply migrations twice")
}

func TestSQLiteBootstrap_PragmasAndConnectionLimits(t *testing.T) {
	database, _ := createTempSQLiteDB(t)

	var fkEnabled int
	err := database.Raw("PRAGMA foreign_keys").Scan(&fkEnabled).Error
	require.NoError(t, err)
	assert.Equal(t, 1, fkEnabled, "foreign keys PRAGMA must be enabled (1)")

	var journalMode string
	err = database.Raw("PRAGMA journal_mode").Scan(&journalMode).Error
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode, "journal mode PRAGMA must be WAL")

	var busyTimeout int
	err = database.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error
	require.NoError(t, err)
	assert.GreaterOrEqual(t, busyTimeout, 10000, "busy timeout PRAGMA must be at least 10000ms")

	sqlDB, err := database.DB()
	require.NoError(t, err)
	stats := sqlDB.Stats()
	assert.Equal(t, 5, stats.MaxOpenConnections, "conservative open connection limit must be configured")
}

func TestSQLiteBootstrap_RetainedDomainBaselineSchemaOnly(t *testing.T) {
	database, _ := createTempSQLiteDB(t)
	runner := migrations.NewSQLiteMigrationRunner(database)
	require.NoError(t, runner.RunMigrations())

	var tables []string
	err := database.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error
	require.NoError(t, err)

	tableMap := make(map[string]bool)
	for _, tbl := range tables {
		tableMap[tbl] = true
	}

	retainedTables := []string{
		"users",
		"chats",
		"antiflood_settings",
		"antiraid_settings",
		"approved_users",
		"blacklists",
		"channels",
		"connection",
		"filters",
		"greetings",
		"notes_settings",
		"notes",
		"reactions",
		"warns_settings",
		"warn_events",
		"schema_migrations",
	}

	for _, tbl := range retainedTables {
		assert.True(t, tableMap[tbl], "retained table %q must exist in SQLite baseline", tbl)
	}

	deletedTables := []string{
		"admin_settings",
		"captcha_settings",
		"captcha_attempts",
		"captcha_muted_users",
		"stored_messages",
		"dev_settings",
		"disable_settings",
		"disable_chat_settings",
		"pin_settings",
		"rules_settings",
	}

	for _, tbl := range deletedTables {
		assert.False(t, tableMap[tbl], "deleted feature table %q must NOT exist in SQLite baseline", tbl)
	}
}

func TestSQLiteBootstrap_ForeignKeyEnforcement(t *testing.T) {
	database, _ := createTempSQLiteDB(t)
	runner := migrations.NewSQLiteMigrationRunner(database)
	require.NoError(t, runner.RunMigrations())

	// Try inserting antiflood_settings for a non-existent chat_id
	invalidFKStmt := "INSERT INTO antiflood_settings (chat_id, flood_limit, action) VALUES (999999, 5, 'mute');"
	err := database.Exec(invalidFKStmt).Error
	require.Error(t, err, "foreign key constraint must reject insert for non-existent chat_id")

	// Insert valid chat
	err = database.Exec("INSERT INTO chats (chat_id, chat_name) VALUES (12345, 'Test Group');").Error
	require.NoError(t, err)

	// Insert antiflood_settings with valid chat_id
	err = database.Exec("INSERT INTO antiflood_settings (chat_id, flood_limit, action) VALUES (12345, 5, 'mute');").Error
	require.NoError(t, err)

	// Delete chat -> should cascade delete antiflood_settings
	err = database.Exec("DELETE FROM chats WHERE chat_id = 12345;").Error
	require.NoError(t, err)

	var afCount int64
	err = database.Table("antiflood_settings").Where("chat_id = ?", 12345).Count(&afCount).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), afCount, "ON DELETE CASCADE must remove child records upon parent deletion")
}

func TestSQLiteBootstrap_HealthReadiness(t *testing.T) {
	database, _ := createTempSQLiteDB(t)
	runner := migrations.NewSQLiteMigrationRunner(database)
	require.NoError(t, runner.RunMigrations())

	sqlDB, err := database.DB()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, sqlDB.PingContext(ctx), "SQLite database must respond cleanly to health ping")
}

func TestSQLiteBootstrap_NoExternalMigrationDirectoryRequired(t *testing.T) {
	// Verify NewSQLiteMigrationRunner works regardless of working directory
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	dbPath := filepath.Join(dir, "no_ext_dir.db")
	database, err := gorm.Open(sqlite.Open(formatSQLiteDSN(dbPath)), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := migrations.NewSQLiteMigrationRunner(database)
	err = runner.RunMigrations()
	require.NoError(t, err, "migration runner must execute embedded migrations without requiring external directory")
}

func TestSQLiteModeDetection(t *testing.T) {
	t.Run("SQLITE_PATH set", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "/data/bot.db")
		t.Setenv("DATABASE_URL", "")
		assert.True(t, IsSQLiteMode())
	})

	t.Run("DATABASE_URL with sqlite prefix", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "")
		t.Setenv("DATABASE_URL", "sqlite:///data/bot.db")
		assert.True(t, IsSQLiteMode())
	})

	t.Run("DATABASE_URL with .db extension", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "")
		t.Setenv("DATABASE_URL", "mydata.db")
		assert.True(t, IsSQLiteMode())
	})

	t.Run("PostgreSQL DSN", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "")
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/dbname")
		assert.False(t, IsSQLiteMode())
	})
}
