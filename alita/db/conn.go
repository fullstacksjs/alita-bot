package db

import (
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/config"
	"github.com/divkix/Alita_Robot/alita/db/migrations"
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

// IsSQLiteMode reports whether SQLite mode is configured via SQLITE_PATH or DATABASE_URL.
func IsSQLiteMode() bool {
	if os.Getenv("SQLITE_PATH") != "" {
		return true
	}
	dsn := ""
	if config.AppConfig != nil && config.AppConfig.DatabaseURL != "" {
		dsn = config.AppConfig.DatabaseURL
	} else {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		return false
	}
	return strings.HasPrefix(dsn, "sqlite") || strings.HasSuffix(dsn, ".db") || strings.HasSuffix(dsn, ".sqlite")
}

// formatSQLiteDSN prepares a SQLite connection string with WAL mode, busy timeout, and foreign keys.
func formatSQLiteDSN(rawDSN string) string {
	pathStr := rawDSN
	if strings.HasPrefix(pathStr, "sqlite://") {
		pathStr = strings.TrimPrefix(pathStr, "sqlite://")
	} else if strings.HasPrefix(pathStr, "sqlite:") {
		pathStr = strings.TrimPrefix(pathStr, "sqlite:")
	}

	if strings.Contains(pathStr, "?") {
		params := []string{}
		if !strings.Contains(pathStr, "_busy_timeout=") {
			params = append(params, "_busy_timeout=10000")
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

	return pathStr + "?_busy_timeout=10000&_journal_mode=WAL&_foreign_keys=ON"
}

func init() {
	if isCliModeActive() {
		return
	}
	if os.Getenv("DATABASE_URL") == "" && os.Getenv("SQLITE_PATH") == "" {
		return
	}

	var err error

	gormLogger := logger.New(
		log.StandardLogger(),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	dsn := ""
	if config.AppConfig != nil && config.AppConfig.DatabaseURL != "" {
		dsn = config.AppConfig.DatabaseURL
	}
	if dsn == "" {
		dsn = os.Getenv("SQLITE_PATH")
	}
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}

	isSQLite := IsSQLiteMode()

	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		if isSQLite {
			sqliteDSN := formatSQLiteDSN(dsn)
			DB, err = gorm.Open(sqlite.Open(sqliteDSN), &gorm.Config{
				Logger:      gormLogger,
				PrepareStmt: true,
				NowFunc: func() time.Time {
					return time.Now().UTC()
				},
			})
		} else {
			DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
				Logger:      gormLogger,
				PrepareStmt: true,
				NowFunc: func() time.Time {
					return time.Now().UTC()
				},
			})
		}
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

	if isSQLite {
		DB.Exec("PRAGMA foreign_keys = ON;")
		DB.Exec("PRAGMA journal_mode = WAL;")
		DB.Exec("PRAGMA busy_timeout = 10000;")
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("[Database][SQL DB]: %v", err)
	}

	if isSQLite {
		maxOpen := 5
		if config.AppConfig != nil && config.AppConfig.DBMaxOpenConns > 0 && config.AppConfig.DBMaxOpenConns <= 5 {
			maxOpen = config.AppConfig.DBMaxOpenConns
		}
		maxIdle := 5
		if config.AppConfig != nil && config.AppConfig.DBMaxIdleConns > 0 && config.AppConfig.DBMaxIdleConns <= 5 {
			maxIdle = config.AppConfig.DBMaxIdleConns
		}
		sqlDB.SetMaxOpenConns(maxOpen)
		sqlDB.SetMaxIdleConns(maxIdle)
		if config.AppConfig != nil {
			sqlDB.SetConnMaxLifetime(time.Duration(config.AppConfig.DBConnMaxLifetimeMin) * time.Minute)
			sqlDB.SetConnMaxIdleTime(time.Duration(config.AppConfig.DBConnMaxIdleTimeMin) * time.Minute)
		}
	} else {
		if config.AppConfig != nil {
			sqlDB.SetMaxIdleConns(config.AppConfig.DBMaxIdleConns)
			sqlDB.SetMaxOpenConns(config.AppConfig.DBMaxOpenConns)
			sqlDB.SetConnMaxLifetime(time.Duration(config.AppConfig.DBConnMaxLifetimeMin) * time.Minute)
			sqlDB.SetConnMaxIdleTime(time.Duration(config.AppConfig.DBConnMaxIdleTimeMin) * time.Minute)
		}
	}

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("[Database][Ping]: %v", err)
	}

	if isSQLite {
		log.Info("Connected to SQLite database successfully!")
		log.Info("[Database] Running embedded SQLite baseline migrations...")
		runner := migrations.NewSQLiteMigrationRunner(DB)
		if err := runner.RunMigrations(); err != nil {
			if config.AppConfig != nil && config.AppConfig.AutoMigrateSilentFail {
				log.Errorf("[Database][AutoMigrate] SQLite migration failed but continuing: %v", err)
			} else {
				log.Fatalf("[Database][AutoMigrate] SQLite migration failed: %v", err)
			}
		} else {
			log.Info("[Database][AutoMigrate] SQLite baseline migrations applied successfully")
		}
	} else {
		log.Info("Connected to PostgreSQL database successfully!")
		if config.AppConfig != nil && config.AppConfig.AutoMigrate {
			log.Info("[Database] AUTO_MIGRATE is enabled, running database migrations...")
			runner := migrations.NewMigrationRunner(DB)
			if err := runner.RunMigrations(); err != nil {
				if config.AppConfig.AutoMigrateSilentFail {
					log.Errorf("[Database][AutoMigrate] Migration failed but continuing: %v", err)
				} else {
					log.Fatalf("[Database][AutoMigrate] Migration failed: %v", err)
				}
			} else {
				log.Info("[Database][AutoMigrate] All migrations applied successfully")
			}
		} else {
			log.Info("Database schema managed via SQL migrations - skipping auto-migration")
		}
	}
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
