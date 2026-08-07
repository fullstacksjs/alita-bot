package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/divkix/Alita_Robot/alita/db"
	"gorm.io/gorm"
)

type OrphanReport struct {
	Table string
	Count int64
	Issue string
	SQL   string
}

type orphanCheck struct {
	table     string
	condition string
	issue     string
	cleanup   string
}

func main() {
	os.Exit(runOrphanValidation(db.DB, os.Stdout))
}

func defaultOrphanChecks() []orphanCheck {
	// Keep this list in sync with the FOREIGN KEY relations declared in
	// alita/db/migrations/sqlite/20260806000000_sqlite_baseline.sql.
	// The channels table intentionally has no chat_id FK: it stores a
	// channel's own Telegram ID, not a reference to a row in chats.
	chatOwnedTables := []string{
		"antiflood_settings",
		"antiraid_settings",
		"blacklists",
		"filters",
		"greetings",
		"notes",
		"notes_settings",
		"reactions",
		"warns_settings",
	}

	checks := make([]orphanCheck, 0, len(chatOwnedTables)+3)
	for _, table := range chatOwnedTables {
		checks = append(checks, orphanCheck{
			table:     table,
			condition: "chat_id NOT IN (SELECT chat_id FROM chats)",
			issue:     "Records with non-existent chat_id",
			cleanup:   "DELETE FROM " + table + " WHERE chat_id NOT IN (SELECT chat_id FROM chats);",
		})
	}

	for _, table := range []string{"approved_users", "connection", "warn_events"} {
		checks = append(checks, orphanCheck{
			table: table,
			condition: "chat_id NOT IN (SELECT chat_id FROM chats) OR " +
				"user_id NOT IN (SELECT user_id FROM users)",
			issue: "Records with non-existent chat_id or user_id",
			cleanup: "DELETE FROM " + table + " WHERE chat_id NOT IN (SELECT chat_id FROM chats) " +
				"OR user_id NOT IN (SELECT user_id FROM users);",
		})
	}

	return checks
}

func findOrphanReports(database *gorm.DB, checks []orphanCheck) ([]OrphanReport, error) {
	reports := make([]OrphanReport, 0, len(checks))
	for _, check := range checks {
		var count int64
		err := database.Table(check.table).Where(check.condition).Count(&count).Error
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", check.table, err)
		}
		if count == 0 {
			continue
		}

		reports = append(reports, OrphanReport{
			Table: check.table,
			Count: count,
			Issue: check.issue,
			SQL:   check.cleanup,
		})
	}
	return reports, nil
}

func runOrphanValidation(database *gorm.DB, out io.Writer) int {
	// Database initialization happens in db package init.
	if database == nil {
		log.Print("[Validation] Database not initialized. Set SQLITE_PATH environment variable.")
		return 1
	}

	reports, err := findOrphanReports(database, defaultOrphanChecks())
	if err != nil {
		log.Printf("[Validation] %v", err)
		return 1
	}

	if _, err := fmt.Fprint(out, formatOrphanReport(reports)); err != nil {
		log.Printf("[Validation] Failed to write report: %v", err)
		return 1
	}

	// Display results
	if len(reports) == 0 {
		return 0
	}

	return 1
}

func formatOrphanReport(reports []OrphanReport) string {
	var builder strings.Builder
	builder.WriteString("🔍 Database Orphaned Data Validation\n")
	builder.WriteString(strings.Repeat("=", 37))
	builder.WriteString("\n")

	if len(reports) == 0 {
		builder.WriteString("✅ No orphaned records found - database is clean!\n")
		return builder.String()
	}

	builder.WriteString("\n❌ Found ")
	builder.WriteString(strconv.Itoa(len(reports)))
	builder.WriteString(" types of orphaned records:\n\n")
	for i, report := range reports {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". Table: ")
		builder.WriteString(report.Table)
		builder.WriteString("\n   Orphaned Records: ")
		builder.WriteString(strconv.FormatInt(report.Count, 10))
		builder.WriteString("\n   Issue: ")
		builder.WriteString(report.Issue)
		builder.WriteString("\n   Cleanup SQL:\n   ")
		builder.WriteString(report.SQL)
		builder.WriteString("\n\n")
	}

	builder.WriteString("⚠️  WARNING: Orphaned records detected!\n")
	builder.WriteString("   Before relying on foreign key constraints, you must:\n")
	builder.WriteString("   1. Review the orphaned records above\n")
	builder.WriteString("   2. Run the cleanup SQL in a transaction\n")
	builder.WriteString("   3. Re-run this validation script to confirm\n")
	builder.WriteString("\n   ⚠️  CRITICAL: Always run cleanup in transactions for safety!\n")
	builder.WriteString("\n   Example cleanup transaction:\n")
	builder.WriteString("   sqlite3 \"$SQLITE_PATH\" << 'EOF'\n")
	builder.WriteString("   BEGIN;\n")
	for _, report := range reports {
		builder.WriteString("   ")
		builder.WriteString(report.SQL)
		builder.WriteString("\n")
	}
	builder.WriteString("   COMMIT;\n")
	builder.WriteString("   EOF\n")
	return builder.String()
}
