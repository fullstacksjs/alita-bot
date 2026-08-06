package stats

import (
	"strings"
	"testing"

	"github.com/divkix/Alita_Robot/alita/db"
)

func skipIfNoDb(t *testing.T) {
	if db.DB == nil {
		t.Skip("DB not initialized")
	}
}

// ---------------------------------------------------------------------------
// LoadAllStats
// ---------------------------------------------------------------------------

func TestLoadAllStats(t *testing.T) {
	skipIfNoDb(t)

	stats := LoadAllStats()
	if stats == "" {
		t.Fatal("LoadAllStats() returned empty string, want non-empty HTML stats")
	}

	// Verify expected sections are present
	expectedSections := []string{
		"Alita's Stats",
		"Deployment Mode",
		"Go Version",
		"Goroutines",
		"Antiflood",
		"Users",
		"Group Activity Metrics",
		"Daily Active Groups",
		"Weekly Active Groups",
		"Monthly Active Groups",
		"User Activity Metrics",
		"Daily Active Users",
		"Weekly Active Users",
		"Monthly Active Users",
		"Pins",
		"CleanLinked Enabled",
		"AntiChannelPin Enabled",
		"Rules",
		"Set",
		"Private",
		"Blacklists",
		"Connections",
		"Disabling",
		"Filters",
		"Greetings",
		"Welcome Enabled",
		"CleanService",
		"CleanWelcome",
		"Notes",
		"Channels Stored",
	}

	for _, section := range expectedSections {
		if !strings.Contains(stats, section) {
			t.Errorf("LoadAllStats() missing expected section %q", section)
		}
	}
}
