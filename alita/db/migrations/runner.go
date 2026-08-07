package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const sqliteMigrationsDir = "sqlite"

// MigrationRunner applies ordered embedded SQLite migrations.
type MigrationRunner struct {
	db   *gorm.DB
	fsys fs.FS
	dir  string
}

// SchemaMigration represents a migration record in the database.
type SchemaMigration struct {
	Version    string    `gorm:"primaryKey;column:version"`
	ExecutedAt time.Time `gorm:"column:executed_at"`
	Checksum   string    `gorm:"column:checksum"`
}

// TableName returns the table name for schema migrations.
func (SchemaMigration) TableName() string {
	return "schema_migrations"
}

// NewSQLiteMigrationRunner creates a runner for the embedded SQLite migrations.
func NewSQLiteMigrationRunner(db *gorm.DB) *MigrationRunner {
	return &MigrationRunner{
		db:   db,
		fsys: SQLiteMigrationsFS,
		dir:  sqliteMigrationsDir,
	}
}

// RunMigrations executes all pending embedded SQLite migrations.
func (m *MigrationRunner) RunMigrations() error {
	log.Info("[Migrations] Starting SQLite database migration...")

	if err := m.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	files, err := m.getMigrationFiles()
	if err != nil {
		return fmt.Errorf("failed to get migration files: %w", err)
	}
	if len(files) == 0 {
		log.Info("[Migrations] No migration files found")
		return nil
	}

	log.Infof("[Migrations] Found %d migration files", len(files))

	applied := 0
	skipped := 0
	for _, file := range files {
		version := path.Base(file)
		content, err := fs.ReadFile(m.fsys, file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", version, err)
		}

		isApplied, err := m.isMigrationApplied(version)
		if err != nil {
			return fmt.Errorf("failed to check migration %s status: %w", version, err)
		}
		if isApplied {
			if err := m.verifyMigrationChecksum(version, content); err != nil {
				return err
			}
			log.Debugf("[Migrations] Skipping %s (already applied)", version)
			skipped++
			continue
		}

		log.Infof("[Migrations] Applying %s...", version)
		if err := m.applyMigration(file, version, content); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", version, err)
		}
		applied++
		log.Infof("[Migrations] Successfully applied %s", version)
	}

	log.Infof("[Migrations] Migration complete - Applied: %d, Skipped: %d", applied, skipped)
	m.logMigrationStatus()
	return nil
}

func (m *MigrationRunner) ensureMigrationsTable() error {
	createSQL := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			executed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			checksum TEXT
		)
	`
	return m.db.Exec(createSQL).Error
}

func (m *MigrationRunner) getMigrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(m.fsys, m.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations dir: %w", err)
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".rollback.sql") {
			continue
		}
		files = append(files, path.Join(m.dir, name))
	}
	slices.Sort(files)
	return files, nil
}

func (m *MigrationRunner) isMigrationApplied(version string) (bool, error) {
	var count int64
	if err := m.db.Model(&SchemaMigration{}).Where("version = ?", version).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *MigrationRunner) getMigrationRecord(version string) (*SchemaMigration, error) {
	var rec SchemaMigration
	if err := m.db.Where("version = ?", version).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

func (m *MigrationRunner) verifyMigrationChecksum(version string, content []byte) error {
	rec, err := m.getMigrationRecord(version)
	if err != nil {
		return fmt.Errorf("failed to read migration record %s: %w", version, err)
	}
	if rec == nil {
		return fmt.Errorf("migration record %s not found", version)
	}

	sum := sha256.Sum256(content)
	current := hex.EncodeToString(sum[:])

	if rec.Checksum == "" {
		if err := m.db.Model(&SchemaMigration{}).
			Where("version = ?", version).
			Update("checksum", current).Error; err != nil {
			return fmt.Errorf("failed to backfill checksum for %s: %w", version, err)
		}
		log.Debugf("[Migrations] Backfilled checksum for legacy migration %s", version)
		return nil
	}

	if rec.Checksum == current {
		return nil
	}

	return fmt.Errorf("migration %s has been modified after it was applied (checksum mismatch); "+
		"migrations are immutable once applied — create a new migration file instead", version)
}

// splitSQLStatements splits SQL on statement-terminating semicolons while
// respecting quoted strings and SQL comments.
func (m *MigrationRunner) splitSQLStatements(sql string) []string {
	var statements []string
	var currentStmt strings.Builder

	runes := []rune(sql)
	length := len(runes)

	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < length; i++ {
		char := runes[i]
		nextChar := rune(0)
		if i+1 < length {
			nextChar = runes[i+1]
		}

		if !inSingleQuote && !inDoubleQuote && !inBlockComment {
			if char == '-' && nextChar == '-' {
				inLineComment = true
				currentStmt.WriteRune(char)
				continue
			}
		}
		if inLineComment {
			currentStmt.WriteRune(char)
			if char == '\n' {
				inLineComment = false
			}
			continue
		}

		if !inSingleQuote && !inDoubleQuote && !inLineComment {
			if char == '/' && nextChar == '*' {
				inBlockComment = true
				currentStmt.WriteRune(char)
				continue
			}
		}
		if inBlockComment {
			currentStmt.WriteRune(char)
			if char == '*' && nextChar == '/' {
				currentStmt.WriteRune(nextChar)
				i++
				inBlockComment = false
			}
			continue
		}

		if !inDoubleQuote && !inLineComment && !inBlockComment {
			if char == '\'' {
				currentStmt.WriteRune(char)
				if inSingleQuote && nextChar == '\'' {
					currentStmt.WriteRune(nextChar)
					i++
					continue
				}
				inSingleQuote = !inSingleQuote
				continue
			}
		}

		if !inSingleQuote && !inLineComment && !inBlockComment {
			if char == '"' {
				currentStmt.WriteRune(char)
				if inDoubleQuote && nextChar == '"' {
					currentStmt.WriteRune(nextChar)
					i++
					continue
				}
				inDoubleQuote = !inDoubleQuote
				continue
			}
		}

		if char == ';' && !inSingleQuote && !inDoubleQuote && !inLineComment && !inBlockComment {
			stmt := strings.TrimSpace(currentStmt.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			currentStmt.Reset()
			continue
		}

		currentStmt.WriteRune(char)
	}

	if leftover := strings.TrimSpace(currentStmt.String()); leftover != "" {
		statements = append(statements, leftover)
	}
	return statements
}

func isTransactionControlStatement(stmt string) bool {
	stmt = strings.TrimSpace(stmt)
	for {
		switch {
		case strings.HasPrefix(stmt, "--"):
			if newline := strings.IndexByte(stmt, '\n'); newline >= 0 {
				stmt = strings.TrimSpace(stmt[newline+1:])
				continue
			}
			return false
		case strings.HasPrefix(stmt, "/*"):
			if end := strings.Index(stmt[2:], "*/"); end >= 0 {
				stmt = strings.TrimSpace(stmt[end+4:])
				continue
			}
			return false
		}
		break
	}

	switch strings.ToUpper(strings.Join(strings.Fields(stmt), " ")) {
	case "BEGIN", "BEGIN WORK", "BEGIN TRANSACTION",
		"START TRANSACTION",
		"COMMIT", "COMMIT WORK", "COMMIT TRANSACTION",
		"END", "END WORK", "END TRANSACTION",
		"ROLLBACK", "ROLLBACK TRANSACTION":
		return true
	default:
		return false
	}
}

func (m *MigrationRunner) applyMigration(filepath, version string, content []byte) error {
	if !strings.HasPrefix(filepath, m.dir+"/") && filepath != m.dir {
		return fmt.Errorf("invalid embedded migration file path: %s", filepath)
	}

	statements := m.splitSQLStatements(string(content))
	statements = slices.DeleteFunc(statements, isTransactionControlStatement)
	if len(statements) == 0 {
		log.Warnf("[Migrations] No statements found in migration %s", version)
		return nil
	}

	tx := m.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := tx.Exec(stmt).Error; err != nil {
			if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
				log.Errorf("[Migrations] Failed to rollback transaction: %v", rollbackErr)
			}
			preview := stmt
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			return fmt.Errorf("failed to execute statement %d/%d: %w\nStatement preview: %s",
				i+1, len(statements), err, preview)
		}
	}

	sum := sha256.Sum256(content)
	migration := SchemaMigration{
		Version:    version,
		ExecutedAt: time.Now().UTC(),
		Checksum:   hex.EncodeToString(sum[:]),
	}
	if err := tx.Create(&migration).Error; err != nil {
		if rollbackErr := tx.Rollback().Error; rollbackErr != nil {
			log.Errorf("[Migrations] Failed to rollback transaction: %v", rollbackErr)
		}
		return fmt.Errorf("failed to record migration: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (m *MigrationRunner) logMigrationStatus() {
	var migrations []SchemaMigration
	if err := m.db.Order("executed_at DESC").Find(&migrations).Error; err != nil {
		log.Errorf("[Migrations] Failed to get migration status: %v", err)
		return
	}
	log.Infof("[Migrations] Currently applied migrations: %d", len(migrations))
	for _, mig := range migrations {
		log.Debugf("[Migrations]   - %s (applied at %s)", mig.Version, mig.ExecutedAt.Format(time.RFC3339))
	}
}
