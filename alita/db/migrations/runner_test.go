package migrations

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newSQLiteTestDB opens a throwaway SQLite database for exercising the
// migration runner in isolation.
func newSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrations_test.db")
	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return database
}

func migrationApplied(t *testing.T, runner *MigrationRunner, version string) bool {
	t.Helper()
	applied, err := runner.isMigrationApplied(version)
	if err != nil {
		t.Fatalf("isMigrationApplied(%q) error = %v", version, err)
	}
	return applied
}

// ---------------------------------------------------------------------------
// SchemaMigration.TableName
// ---------------------------------------------------------------------------

func TestSchemaMigrationTableName(t *testing.T) {
	got := SchemaMigration{}.TableName()
	if got != "schema_migrations" {
		t.Fatalf("SchemaMigration.TableName() = %q, want %q", got, "schema_migrations")
	}
}

// ---------------------------------------------------------------------------
// NewSQLiteMigrationRunner + fresh embedded baseline
// ---------------------------------------------------------------------------

func TestNewSQLiteMigrationRunnerAppliesEmbeddedBaseline(t *testing.T) {
	database := newSQLiteTestDB(t)
	runner := NewSQLiteMigrationRunner(database)

	if err := runner.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	var count int64
	if err := database.Model(&SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one applied migration record")
	}

	var record SchemaMigration
	if err := database.Where("version LIKE ?", "%sqlite_baseline%").First(&record).Error; err != nil {
		t.Fatalf("expected sqlite baseline migration record: %v", err)
	}
	if record.Checksum == "" {
		t.Fatal("expected non-empty checksum for applied migration")
	}

	// Baseline tables must exist after the run.
	for _, table := range []string{"users", "chats", "schema_migrations"} {
		if !database.Migrator().HasTable(table) {
			t.Fatalf("expected table %q to exist after baseline migration", table)
		}
	}
}

// ---------------------------------------------------------------------------
// Idempotent second run / checksum verify
// ---------------------------------------------------------------------------

func TestRunMigrationsIsIdempotent(t *testing.T) {
	database := newSQLiteTestDB(t)
	runner := NewSQLiteMigrationRunner(database)

	if err := runner.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() first run error = %v", err)
	}
	var firstCount int64
	if err := database.Model(&SchemaMigration{}).Count(&firstCount).Error; err != nil {
		t.Fatalf("count schema_migrations after first run: %v", err)
	}

	runner2 := NewSQLiteMigrationRunner(database)
	if err := runner2.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() second run error = %v", err)
	}
	var secondCount int64
	if err := database.Model(&SchemaMigration{}).Count(&secondCount).Error; err != nil {
		t.Fatalf("count schema_migrations after second run: %v", err)
	}

	if firstCount != secondCount {
		t.Fatalf("second run applied migrations again: first=%d second=%d", firstCount, secondCount)
	}
}

func TestRunMigrationsSkipsAlreadyAppliedWithMatchingChecksum(t *testing.T) {
	database := newSQLiteTestDB(t)

	fsys := fstest.MapFS{
		"sqlite/001_create_widgets.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"),
		},
	}
	runner := &MigrationRunner{db: database, fsys: fsys, dir: sqliteMigrationsDir}

	if err := runner.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() first run error = %v", err)
	}
	if !migrationApplied(t, runner, "001_create_widgets.sql") {
		t.Fatal("expected 001_create_widgets.sql to be recorded as applied")
	}

	// Re-run with the exact same content; must be skipped without error.
	runner2 := &MigrationRunner{db: database, fsys: fsys, dir: sqliteMigrationsDir}
	if err := runner2.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() second run error = %v", err)
	}

	var count int64
	if err := database.Table("widgets").Count(&count).Error; err != nil {
		t.Fatalf("count widgets: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Checksum mismatch fails
// ---------------------------------------------------------------------------

func TestRunMigrationsDetectsChecksumMismatch(t *testing.T) {
	database := newSQLiteTestDB(t)

	fsys := fstest.MapFS{
		"sqlite/001_create_gadgets.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"),
		},
	}
	runner := &MigrationRunner{db: database, fsys: fsys, dir: sqliteMigrationsDir}
	if err := runner.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() first run error = %v", err)
	}

	// Simulate the migration file changing after it was applied.
	mutatedFS := fstest.MapFS{
		"sqlite/001_create_gadgets.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY, extra TEXT);"),
		},
	}
	runner2 := &MigrationRunner{db: database, fsys: mutatedFS, dir: sqliteMigrationsDir}
	err := runner2.RunMigrations()
	if err == nil {
		t.Fatal("RunMigrations() error = nil, want checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("RunMigrations() error = %q, want it to mention checksum mismatch", err)
	}
}

func TestLegacyRowWithoutChecksumIsBackfilled(t *testing.T) {
	database := newSQLiteTestDB(t)

	fsys := fstest.MapFS{
		"sqlite/001_legacy.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE legacy_items (id INTEGER PRIMARY KEY);"),
		},
	}
	runner := &MigrationRunner{db: database, fsys: fsys, dir: sqliteMigrationsDir}
	if err := runner.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable() error = %v", err)
	}

	// Insert a legacy record with no checksum, simulating a migration applied
	// before checksum tracking existed.
	if err := database.Create(&SchemaMigration{Version: "001_legacy.sql"}).Error; err != nil {
		t.Fatalf("create legacy record: %v", err)
	}

	if err := runner.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations() with legacy empty-checksum row error = %v", err)
	}

	rec, err := runner.getMigrationRecord("001_legacy.sql")
	if err != nil {
		t.Fatalf("getMigrationRecord() error = %v", err)
	}
	if rec == nil || rec.Checksum == "" {
		t.Fatal("expected legacy record checksum to be backfilled")
	}

	// The table must not have been (re-)created since the migration was
	// already considered applied.
	if database.Migrator().HasTable("legacy_items") {
		t.Fatal("legacy migration should not have been re-applied")
	}
}

// ---------------------------------------------------------------------------
// Transaction rollback on bad migration
// ---------------------------------------------------------------------------

func TestApplyMigrationRollsBackOnFailure(t *testing.T) {
	database := newSQLiteTestDB(t)

	fsys := fstest.MapFS{
		"sqlite/001_bad.sql": &fstest.MapFile{
			Data: []byte(`
CREATE TABLE rollback_items (id INTEGER PRIMARY KEY);
INSERT INTO nonexistent_table (id) VALUES (1);
`),
		},
	}
	runner := &MigrationRunner{db: database, fsys: fsys, dir: sqliteMigrationsDir}
	if err := runner.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable() error = %v", err)
	}

	content, err := fsys.ReadFile("sqlite/001_bad.sql")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	err = runner.applyMigration("sqlite/001_bad.sql", "001_bad.sql", content)
	if err == nil {
		t.Fatal("applyMigration() error = nil, want statement failure")
	}
	if !strings.Contains(err.Error(), "Statement preview") {
		t.Fatalf("applyMigration() error = %q, want statement preview", err)
	}
	if migrationApplied(t, runner, "001_bad.sql") {
		t.Fatal("failing migration was recorded as applied")
	}
	if database.Migrator().HasTable("rollback_items") {
		t.Fatal("failed migration left rollback_items table behind due to missing rollback")
	}
}

func TestApplyMigrationRejectsUnsafePath(t *testing.T) {
	database := newSQLiteTestDB(t)
	runner := &MigrationRunner{db: database, fsys: fstest.MapFS{}, dir: sqliteMigrationsDir}

	err := runner.applyMigration("../outside.sql", "outside.sql", []byte("SELECT 1;"))
	if err == nil {
		t.Fatal("applyMigration() error = nil, want unsafe path rejection")
	}
	if !strings.Contains(err.Error(), "migration file path") {
		t.Fatalf("applyMigration() error = %q, want path validation error", err)
	}
}

func TestApplyMigrationEmptyFileDoesNotRecordVersion(t *testing.T) {
	database := newSQLiteTestDB(t)
	runner := &MigrationRunner{db: database, fsys: fstest.MapFS{}, dir: sqliteMigrationsDir}
	if err := runner.ensureMigrationsTable(); err != nil {
		t.Fatalf("ensureMigrationsTable() error = %v", err)
	}

	version := "002_empty.sql"
	if err := runner.applyMigration(sqliteMigrationsDir+"/"+version, version, []byte("   \n\t  ")); err != nil {
		t.Fatalf("applyMigration(empty) error = %v", err)
	}
	if migrationApplied(t, runner, version) {
		t.Fatal("empty migration was recorded as applied")
	}
}

// ---------------------------------------------------------------------------
// splitSQLStatements
// ---------------------------------------------------------------------------

func TestSplitSQLStatements(t *testing.T) {
	runner := &MigrationRunner{}

	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{name: "simple split", input: "SELECT 1; SELECT 2;", wantCount: 2},
		{
			name: "block comment preserved",
			input: `/* this is a comment; not a statement */
SELECT 1;`,
			wantCount: 1,
		},
		{
			name: "line comment preserved",
			input: `-- this is a comment; not a statement
SELECT 1;`,
			wantCount: 1,
		},
		{name: "quoted semicolons not split", input: `SELECT 'hello; world'; SELECT 2;`, wantCount: 2},
		{name: "empty input returns nothing", input: "", wantCount: 0},
		{name: "whitespace-only returns nothing", input: "   \n\t  ", wantCount: 0},
		{name: "single statement no semicolon", input: "SELECT 1", wantCount: 1},
		{name: "three statements", input: "SELECT 1; SELECT 2; SELECT 3;", wantCount: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runner.splitSQLStatements(tc.input)
			if len(got) != tc.wantCount {
				t.Fatalf("splitSQLStatements() returned %d statements, want %d\nstatements: %v", len(got), tc.wantCount, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isTransactionControlStatement
// ---------------------------------------------------------------------------

func TestIsTransactionControlStatement(t *testing.T) {
	tests := []struct {
		stmt string
		want bool
	}{
		{stmt: "BEGIN", want: true},
		{stmt: "-- migration transaction\nBEGIN", want: true},
		{stmt: "/* migration transaction */ COMMIT", want: true},
		{stmt: "ROLLBACK WORK", want: false},
		{stmt: "START TRANSACTION", want: true},
		{stmt: "SELECT 'COMMIT'", want: false},
		{stmt: "-- BEGIN", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.stmt, func(t *testing.T) {
			if got := isTransactionControlStatement(tc.stmt); got != tc.want {
				t.Fatalf("isTransactionControlStatement(%q) = %t, want %t", tc.stmt, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getMigrationFiles
// ---------------------------------------------------------------------------

func TestGetMigrationFiles(t *testing.T) {
	t.Run("embedded baseline returns sorted sql files", func(t *testing.T) {
		runner := NewSQLiteMigrationRunner(nil)
		files, err := runner.getMigrationFiles()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) == 0 {
			t.Fatal("expected at least one embedded migration file")
		}
		for _, f := range files {
			if !strings.HasSuffix(f, ".sql") {
				t.Errorf("expected only .sql files, got %s", f)
			}
			if strings.HasSuffix(f, ".rollback.sql") {
				t.Errorf("rollback migration should not be executable: %s", f)
			}
		}
	})

	t.Run("rollback sql files are ignored", func(t *testing.T) {
		fsys := fstest.MapFS{
			"sqlite/001_create_table.sql":          &fstest.MapFile{Data: []byte("SELECT 1;")},
			"sqlite/001_create_table.rollback.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
			"sqlite/002_add_column.sql":            &fstest.MapFile{Data: []byte("SELECT 1;")},
		}
		runner := &MigrationRunner{fsys: fsys, dir: sqliteMigrationsDir}
		files, err := runner.getMigrationFiles()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("expected 2 executable migration files, got %d: %v", len(files), files)
		}
		for _, f := range files {
			if strings.HasSuffix(f, ".rollback.sql") {
				t.Fatalf("rollback migration should not be executable: %s", f)
			}
		}
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		runner := &MigrationRunner{fsys: fstest.MapFS{}, dir: "missing"}
		_, err := runner.getMigrationFiles()
		if err == nil {
			t.Fatal("expected error for non-existent embedded directory")
		}
	})
}
