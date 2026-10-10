package channels

import (
	"fmt"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/divkix/Alita_Robot/alita/db"
	dbcache "github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/migrations"
	"github.com/divkix/Alita_Robot/alita/db/models"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

func TestUpdateChannelRetriesStaleSnapshot(t *testing.T) {
	t.Cleanup(dbcache.SetEnabled(false))
	state.SimulateRestart()
	t.Cleanup(state.SimulateRestart)
	for _, username := range []string{"newsroom", ""} {
		t.Run(fmt.Sprintf("username=%q", username), func(t *testing.T) {
			dsn := db.FormatSQLiteDSN(filepath.Join(t.TempDir(), "channels.db"))
			open := func() *gorm.DB {
				t.Helper()
				database, err := db.OpenSQLite(dsn, logger.Default.LogMode(logger.Silent))
				if err != nil {
					t.Fatalf("open SQLite: %v", err)
				}
				sqlDB, err := database.DB()
				if err != nil {
					t.Fatalf("get SQLite handle: %v", err)
				}
				t.Cleanup(func() { _ = sqlDB.Close() })
				return database
			}
			database := open()
			if err := migrations.NewSQLiteMigrationRunner(database).RunMigrations(); err != nil {
				t.Fatalf("migrate SQLite: %v", err)
			}
			writer := open()
			originalDB := db.DB
			db.DB = database
			t.Cleanup(func() { db.DB = originalDB })

			const oldID, newID = int64(-1000000000001), int64(-1000000000002)
			if username != "" {
				if err := database.Create(&models.ChannelSettings{ChatId: oldID, Username: username}).Error; err != nil {
					t.Fatalf("seed username owner: %v", err)
				}
			}

			// Commit a competing write after each of the first three reads. WAL
			// then rejects promotion of that transaction's snapshot to a writer,
			// even with the production busy timeout enabled.
			conflicts := 0
			if err := database.Callback().Query().After("gorm:query").Register("test:competing_writer", func(tx *gorm.DB) {
				if tx.Statement.Table != "channels" || tx.Error != nil || conflicts >= 3 {
					return
				}
				conflicts++
				if err := writer.Create(&models.ChannelSettings{ChatId: int64(-2000000000000 - conflicts)}).Error; err != nil {
					t.Errorf("competing write: %v", err)
				}
			}); err != nil {
				t.Fatalf("register competing writer: %v", err)
			}

			if err := UpdateChannel(newID, "News", username); err != nil {
				t.Fatalf("UpdateChannel after lock conflicts: %v", err)
			}
			if conflicts != 3 {
				t.Fatalf("competing writes = %d, want 3", conflicts)
			}
			var channel models.ChannelSettings
			if err := database.Where("chat_id = ?", newID).First(&channel).Error; err != nil {
				t.Fatalf("read stored channel: %v", err)
			}
			if channel.ChannelId != newID || channel.ChannelName != "News" || channel.Username != username {
				t.Fatalf("unexpected stored channel: %+v", channel)
			}
			if username != "" {
				var old models.ChannelSettings
				if err := database.Where("chat_id = ?", oldID).First(&old).Error; err != nil {
					t.Fatalf("read previous username owner: %v", err)
				}
				if old.Username != "" {
					t.Fatalf("previous owner's username = %q, want cleared", old.Username)
				}
			}
		})
	}
}
