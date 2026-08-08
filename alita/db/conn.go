package db

import (
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db/migrations"
)

const (
	defaultSQLitePath   = "/data/alita.db"
	sqliteMaxOpenConns  = 5
	sqliteMaxIdleConns  = 5
	sqliteBusyTimeoutMS = 10000
)

var (
	DB *gorm.DB
)

// isCliModeActive returns true if the program is running with CLI flags
// that should skip database initialization (--version, --health, -v). Tests
// also skip it unless ALITA_TEST_DATABASE explicitly opts into database init.
func isCliModeActive() bool {
	testBinary := strings.TrimSuffix(os.Args[0], ".exe")
	if strings.HasSuffix(testBinary, ".test") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv("ALITA_TEST_DATABASE")), "true") {
		return true
	}
	if len(os.Args) < 2 {
		return false
	}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-version", "-v", "--health", "-health":
			return true
		}
	}
	return false
}

// ResolveSQLitePath returns the configured SQLite file path, defaulting to
// /data/alita.db when neither config nor SQLITE_PATH is set.
func ResolveSQLitePath() string {
	if config.AppConfig != nil && config.AppConfig.SQLitePath != "" {
		return config.AppConfig.SQLitePath
	}
	if path := strings.TrimSpace(os.Getenv("SQLITE_PATH")); path != "" {
		return path
	}
	return defaultSQLitePath
}

// FormatSQLiteDSN prepares a SQLite connection string with WAL mode, busy timeout, and foreign keys.
func FormatSQLiteDSN(rawDSN string) string {
	pathStr := rawDSN
	switch {
	case strings.HasPrefix(pathStr, "sqlite://"):
		pathStr = strings.TrimPrefix(pathStr, "sqlite://")
	case strings.HasPrefix(pathStr, "sqlite:"):
		pathStr = strings.TrimPrefix(pathStr, "sqlite:")
	}

	if strings.Contains(pathStr, "?") {
		params := []string{}
		if !strings.Contains(pathStr, "_busy_timeout=") {
			params = append(params, fmt.Sprintf("_busy_timeout=%d", sqliteBusyTimeoutMS))
		}
		if !strings.Contains(pathStr, "_journal_mode=") {
			params = append(params, "_journal_mode=WAL")
		}
		if !strings.Contains(pathStr, "_foreign_keys=") && !strings.Contains(pathStr, "_fk=") {
			params = append(params, "_foreign_keys=ON")
		}
		if len(params) > 0 {
			pathStr += "&" + strings.Join(params, "&")
		}
		return pathStr
	}

	return fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=WAL&_foreign_keys=ON", pathStr, sqliteBusyTimeoutMS)
}

// OpenSQLite opens one SQLite database with the settings the running bot uses:
// the shared PRAGMAs and the connection limits suited to SQLite's single-writer
// model. Tests that must reproduce production connection behavior call it
// instead of opening GORM themselves.
//
// Prepared-statement caching stays off. GORM's cache prepares statements on the
// connection (and, inside a transaction, on the transaction) and closes them
// only afterwards, so go-sqlite3 rejects the COMMIT with "cannot commit
// transaction - SQL statements in progress". That breaks every transaction,
// starting with the embedded migrations a container applies to a fresh /data
// volume.
func OpenSQLite(dsn string, gormLogger logger.Interface) (*gorm.DB, error) {
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:      gormLogger,
		PrepareStmt: false,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, err
	}

	database.Exec("PRAGMA foreign_keys = ON;")
	database.Exec("PRAGMA journal_mode = WAL;")
	database.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d;", sqliteBusyTimeoutMS))

	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConns)
	sqlDB.SetMaxIdleConns(sqliteMaxIdleConns)

	return database, nil
}

func init() {
	if isCliModeActive() {
		return
	}
	// Unit tests and package imports without a real bot process leave the path
	// unset; production LoadConfig always supplies SQLitePath (or its default).
	if config.AppConfig == nil || config.AppConfig.BotToken == "" {
		if os.Getenv("SQLITE_PATH") == "" && os.Getenv("ALITA_TEST_DATABASE") == "" {
			return
		}
	}

	gormLogger := logger.New(
		log.StandardLogger(),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	dsn := FormatSQLiteDSN(ResolveSQLitePath())
	var err error
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		DB, err = OpenSQLite(dsn, gormLogger)
		if err == nil {
			break
		}

		log.WithFields(log.Fields{
			"attempt": attempt + 1,
			"error":   err,
		}).Warning("[Database][Connection] Failed to connect, retrying...")

		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}
	if err != nil {
		log.Fatalf("[Database][Connection] Failed after %d attempts: %v", maxRetries, err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("[Database][SQL DB]: %v", err)
	}

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("[Database][Ping]: %v", err)
	}

	log.Info("Connected to SQLite database successfully!")
	log.Info("[Database] Running embedded SQLite migrations...")
	runner := migrations.NewSQLiteMigrationRunner(DB)
	if err := runner.RunMigrations(); err != nil {
		log.Fatalf("[Database][Migrate] SQLite migration failed: %v", err)
	}
	log.Info("[Database][Migrate] SQLite migrations applied successfully")
}

// Close closes the database connection gracefully.
func Close() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			return fmt.Errorf("failed to get underlying SQL DB: %w", err)
		}
		return sqlDB.Close()
	}
	return nil
}
