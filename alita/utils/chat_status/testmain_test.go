package chat_status

import (
	"fmt"
	"os"
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
		dbFile, err := os.CreateTemp("", "alita_chat_status_test_*.db")
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
		sqliteDB, err := gorm.Open(
			sqlite.Open(dbPath),
			&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
		)
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

		runner := migrations.NewSQLiteMigrationRunner(sqliteDB)
		if err := runner.RunMigrations(); err != nil {
			fmt.Printf("Migration failed: %v\n", err)
			os.Exit(1)
		}

		db.DB = sqliteDB
	}

	exitCode := m.Run()

	if db.DB != nil {
		if sqlDB, err := db.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if dbFileName != "" {
		_ = os.Remove(dbFileName)
	}
	os.Exit(exitCode)
}
