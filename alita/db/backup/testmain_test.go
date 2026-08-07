package backup

import (
	"os"
	"testing"

	"github.com/divkix/Alita_Robot/alita/db"
	"github.com/divkix/Alita_Robot/alita/db/testsqlite"
)

func TestMain(m *testing.M) {
	var cleanup func()
	if db.DB == nil {
		db.DB, cleanup = testsqlite.MustOpen()
	}

	exitCode := m.Run()

	if cleanup != nil {
		cleanup()
	}

	os.Exit(exitCode)
}
