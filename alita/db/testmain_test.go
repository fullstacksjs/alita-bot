package db

import (
	"os"
	"testing"

	"github.com/divkix/Alita_Robot/alita/db/testsqlite"
)

func TestMain(m *testing.M) {
	var cleanup func()
	if DB == nil {
		DB, cleanup = testsqlite.MustOpen()
	}

	exitCode := m.Run()

	if cleanup != nil {
		cleanup()
	}

	os.Exit(exitCode)
}

func skipIfNoDb(t *testing.T) {
	t.Helper()
	if DB == nil {
		t.Skip("requires database connection")
	}
}
