package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db/migrations"
)

func createTempSQLiteDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_sqlite.db")
	dsn := FormatSQLiteDSN(dbPath)

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

// TestSQLiteBootstrap_ProductionConfigCommitsTransactions opens a temporary
// database exactly the way the running bot does. It guards the container
// bootstrap: with GORM's prepared-statement cache enabled, statements stay open
// on the transaction and go-sqlite3 rejects the COMMIT, so a container starting
// from an empty /data volume dies while applying the embedded migrations.
func TestSQLiteBootstrap_ProductionConfigCommitsTransactions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "production_config.db")

	database, err := OpenSQLite(FormatSQLiteDSN(dbPath), nil)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	runner := migrations.NewSQLiteMigrationRunner(database)
	require.NoError(t, runner.RunMigrations(), "embedded migrations must commit under the production GORM config")

	err = database.Transaction(func(tx *gorm.DB) error {
		return tx.Create(&migrations.SchemaMigration{
			Version:    "99999999999999_transaction_probe",
			ExecutedAt: time.Now().UTC(),
			Checksum:   "probe",
		}).Error
	})
	require.NoError(t, err, "ordinary writes must commit under the production GORM config")

	var count int64
	require.NoError(t, database.Model(&migrations.SchemaMigration{}).
		Where("version = ?", "99999999999999_transaction_probe").Count(&count).Error)
	assert.Equal(t, int64(1), count, "the committed row must be readable after the transaction")
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

	dbPath := filepath.Join(dir, "no_ext_dir.db")
	database, err := gorm.Open(sqlite.Open(FormatSQLiteDSN(dbPath)), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := migrations.NewSQLiteMigrationRunner(database)
	err = runner.RunMigrations()
	require.NoError(t, err, "migration runner must execute embedded migrations without requiring external directory")
}

// ---------------------------------------------------------------------------
// ResolveSQLitePath
// ---------------------------------------------------------------------------

func TestResolveSQLitePath(t *testing.T) {
	originalConfig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = originalConfig })

	t.Run("defaults to /data/alita.db when unset", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "")
		config.AppConfig = &config.Config{}

		assert.Equal(t, "/data/alita.db", ResolveSQLitePath())
	})

	t.Run("SQLITE_PATH env var wins when config is empty", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "/tmp/env-path.db")
		config.AppConfig = &config.Config{}

		assert.Equal(t, "/tmp/env-path.db", ResolveSQLitePath())
	})

	t.Run("config.SQLitePath takes precedence over env var", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "/tmp/env-path.db")
		config.AppConfig = &config.Config{SQLitePath: "/tmp/config-path.db"}

		assert.Equal(t, "/tmp/config-path.db", ResolveSQLitePath())
	})

	t.Run("nil AppConfig falls back to env var", func(t *testing.T) {
		t.Setenv("SQLITE_PATH", "/tmp/env-path.db")
		config.AppConfig = nil

		assert.Equal(t, "/tmp/env-path.db", ResolveSQLitePath())
	})
}

// ---------------------------------------------------------------------------
// FormatSQLiteDSN
// ---------------------------------------------------------------------------

func TestFormatSQLiteDSN(t *testing.T) {
	t.Run("plain path gets default pragmas appended", func(t *testing.T) {
		dsn := FormatSQLiteDSN("/data/alita.db")
		assert.Contains(t, dsn, "_busy_timeout=")
		assert.Contains(t, dsn, "_journal_mode=WAL")
		assert.Contains(t, dsn, "_foreign_keys=ON")
	})

	t.Run("sqlite:// prefix is stripped", func(t *testing.T) {
		dsn := FormatSQLiteDSN("sqlite:///data/alita.db")
		assert.NotContains(t, dsn, "sqlite://")
	})

	t.Run("sqlite: prefix is stripped", func(t *testing.T) {
		dsn := FormatSQLiteDSN("sqlite:/data/alita.db")
		assert.NotContains(t, dsn, "sqlite:")
	})

	t.Run("existing query params are preserved and merged", func(t *testing.T) {
		dsn := FormatSQLiteDSN("/data/alita.db?_busy_timeout=5000")
		assert.Contains(t, dsn, "_busy_timeout=5000")
		assert.Contains(t, dsn, "_journal_mode=WAL")
		assert.Contains(t, dsn, "_foreign_keys=ON")
	})
}
