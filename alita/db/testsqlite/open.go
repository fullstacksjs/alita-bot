// Package testsqlite provides shared helpers for tests that need a real
// SQLite database migrated to the embedded baseline schema, instead of
// GORM AutoMigrate against ad hoc (and often deleted) model lists.
package testsqlite

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/db/migrations"
)

const (
	maxOpenConns = 5
	maxIdleConns = 5
)

// formatDSN mirrors alita/db.FormatSQLiteDSN without importing package db,
// which would create an import cycle for db's own tests.
func formatDSN(path string) string {
	return fmt.Sprintf("%s?_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=ON", path)
}

// Open creates a temporary SQLite database file, opens it with WAL
// journaling, foreign key enforcement, a busy timeout, and a conservative
// connection pool, then applies the embedded SQLite baseline migrations via
// migrations.NewSQLiteMigrationRunner. It returns the opened *gorm.DB along
// with a cleanup function that closes the connection and removes the temp
// file; callers must invoke cleanup once done.
func Open() (*gorm.DB, func(), error) {
	dbFile, err := os.CreateTemp("", "alita_test_*.db")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp db file: %w", err)
	}
	dbPath := dbFile.Name()
	if err := dbFile.Close(); err != nil {
		return nil, nil, fmt.Errorf("close temp db file: %w", err)
	}
	removeFile := func() { _ = os.Remove(dbPath) }

	gormDB, err := gorm.Open(sqlite.Open(formatDSN(dbPath)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		removeFile()
		return nil, nil, fmt.Errorf("open sqlite database: %w", err)
	}

	gormDB.Exec("PRAGMA foreign_keys = ON;")
	gormDB.Exec("PRAGMA journal_mode = WAL;")
	gormDB.Exec("PRAGMA busy_timeout = 10000;")

	sqlDB, err := gormDB.DB()
	if err != nil {
		removeFile()
		return nil, nil, fmt.Errorf("get underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)

	cleanup := func() {
		_ = sqlDB.Close()
		removeFile()
	}

	if err := migrations.NewSQLiteMigrationRunner(gormDB).RunMigrations(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("run embedded sqlite migrations: %w", err)
	}

	return gormDB, cleanup, nil
}

// MustOpen is like Open but terminates the process via os.Exit(1) on
// failure. It is intended for TestMain harnesses, where returning an error
// is not idiomatic.
func MustOpen() (*gorm.DB, func()) {
	gormDB, cleanup, err := Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return gormDB, cleanup
}

// OpenT is like Open but fails the test immediately via tb.Fatalf on error
// and automatically registers the cleanup function with tb.Cleanup.
func OpenT(tb testing.TB) *gorm.DB {
	tb.Helper()
	gormDB, cleanup, err := Open()
	if err != nil {
		tb.Fatalf("testsqlite.Open: %v", err)
	}
	tb.Cleanup(cleanup)
	return gormDB
}
