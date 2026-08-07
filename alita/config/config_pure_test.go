package config

import (
	"os"
	"strings"
	"testing"
)

// TestIsCliModeActive tests the isCliModeActive helper. It does NOT call
// t.Parallel() because it mutates os.Args and must run sequentially.
func TestIsCliModeActive(t *testing.T) {
	saveArgs := os.Args
	defer func() { os.Args = saveArgs }()

	t.Run("no args returns false", func(t *testing.T) {
		os.Args = []string{"binary"}
		if isCliModeActive() {
			t.Errorf("isCliModeActive() = true, want false")
		}
	})

	t.Run("single positional arg returns false", func(t *testing.T) {
		os.Args = []string{"binary", "start"}
		if isCliModeActive() {
			t.Errorf("isCliModeActive() = true, want false")
		}
	})

	t.Run("--version returns true", func(t *testing.T) {
		os.Args = []string{"binary", "--version"}
		if !isCliModeActive() {
			t.Errorf("isCliModeActive() = false, want true")
		}
	})

	t.Run("-v returns true", func(t *testing.T) {
		os.Args = []string{"binary", "-v"}
		if !isCliModeActive() {
			t.Errorf("isCliModeActive() = false, want true")
		}
	})

	t.Run("--health returns true", func(t *testing.T) {
		os.Args = []string{"binary", "--health"}
		if !isCliModeActive() {
			t.Errorf("isCliModeActive() = false, want true")
		}
	})

	t.Run("mixed args with flag returns true", func(t *testing.T) {
		os.Args = []string{"binary", "run", "--version"}
		if !isCliModeActive() {
			t.Errorf("isCliModeActive() = false, want true")
		}
	})

	t.Run("-version returns true", func(t *testing.T) {
		os.Args = []string{"binary", "-version"}
		if !isCliModeActive() {
			t.Errorf("isCliModeActive() = false, want true")
		}
	})

	t.Run("-health returns true", func(t *testing.T) {
		os.Args = []string{"binary", "-health"}
		if !isCliModeActive() {
			t.Errorf("isCliModeActive() = false, want true")
		}
	})
}

// TestLoadConfig tests the LoadConfig helper. The top-level test does NOT call
// t.Parallel() because t.Setenv() is incompatible with parallel execution.
func TestLoadConfig(t *testing.T) {
	t.Run("returns error when required fields missing", func(t *testing.T) {
		// Ensure required env vars are empty
		t.Setenv("BOT_TOKEN", "")
		t.Setenv("OWNER_ID", "")
		t.Setenv("MESSAGE_DUMP", "")
		t.Setenv("SQLITE_PATH", "")

		_, err := LoadConfig()
		if err == nil {
			t.Fatalf("expected error for missing required fields, got nil")
		}
	})

	t.Run("loads config with all required env vars", func(t *testing.T) {
		t.Setenv("BOT_TOKEN", "test-token")
		t.Setenv("OWNER_ID", "12345")
		t.Setenv("MESSAGE_DUMP", "67890")
		t.Setenv("SQLITE_PATH", "/tmp/test-alita.db")
		t.Setenv("HTTP_PORT", "9090")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.BotToken != "test-token" {
			t.Errorf("BotToken: got %q, want %q", cfg.BotToken, "test-token")
		}
		if cfg.OwnerId != 12345 {
			t.Errorf("OwnerId: got %d, want %d", cfg.OwnerId, 12345)
		}
		if cfg.MessageDump != 67890 {
			t.Errorf("MessageDump: got %d, want %d", cfg.MessageDump, 67890)
		}
		if cfg.SQLitePath != "/tmp/test-alita.db" {
			t.Errorf("SQLitePath: got %q, want %q", cfg.SQLitePath, "/tmp/test-alita.db")
		}
		if cfg.HTTPPort != 9090 {
			t.Errorf("HTTPPort: got %d, want %d", cfg.HTTPPort, 9090)
		}
		// Defaults should have been applied
		if cfg.ApiServer != "https://api.telegram.org" {
			t.Errorf("ApiServer: got %q, want %q", cfg.ApiServer, "https://api.telegram.org")
		}
		if cfg.BotVersion == "" {
			t.Errorf("BotVersion: got empty string, want non-empty")
		}
		// AllowedUpdates should be populated
		if len(cfg.AllowedUpdates) == 0 {
			t.Errorf("AllowedUpdates: expected non-empty slice")
		}
	})

	t.Run("SQLITE_PATH defaults when unset", func(t *testing.T) {
		t.Setenv("BOT_TOKEN", "tk")
		t.Setenv("OWNER_ID", "1")
		t.Setenv("MESSAGE_DUMP", "1")
		t.Setenv("SQLITE_PATH", "")
		t.Setenv("HTTP_PORT", "8080")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SQLitePath != "/data/alita.db" {
			t.Errorf("SQLitePath: got %q, want %q", cfg.SQLitePath, "/data/alita.db")
		}
	})

	t.Run("webhook config loaded correctly", func(t *testing.T) {
		t.Setenv("BOT_TOKEN", "tk")
		t.Setenv("OWNER_ID", "1")
		t.Setenv("MESSAGE_DUMP", "1")
		t.Setenv("SQLITE_PATH", "/tmp/test-alita.db")
		t.Setenv("USE_WEBHOOKS", "true")
		t.Setenv("WEBHOOK_DOMAIN", "example.com")
		t.Setenv("WEBHOOK_SECRET", "shh")
		t.Setenv("HTTP_PORT", "8080")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !cfg.UseWebhooks {
			t.Errorf("UseWebhooks: got false, want true")
		}
		if cfg.WebhookDomain != "example.com" {
			t.Errorf("WebhookDomain: got %q, want %q", cfg.WebhookDomain, "example.com")
		}
		if cfg.WebhookSecret != "shh" {
			t.Errorf("WebhookSecret: got %q, want %q", cfg.WebhookSecret, "shh")
		}
	})

	t.Run("ENABLE_PPROF parsed as bool", func(t *testing.T) {
		t.Setenv("BOT_TOKEN", "tk")
		t.Setenv("OWNER_ID", "1")
		t.Setenv("MESSAGE_DUMP", "1")
		t.Setenv("SQLITE_PATH", "/tmp/test-alita.db")
		t.Setenv("ENABLE_PPROF", "yes")
		t.Setenv("HTTP_PORT", "8080")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.EnablePPROF {
			t.Errorf("EnablePPROF: got false, want true")
		}
	})
}

func TestValidateConfigPure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*Config)
		wantErr string
	}{
		{name: "valid base config", setup: func(_ *Config) {}},
		{
			name:    "missing bot token",
			setup:   func(c *Config) { c.BotToken = "" },
			wantErr: "BOT_TOKEN is required",
		},
		{
			name:    "missing owner",
			setup:   func(c *Config) { c.OwnerId = 0 },
			wantErr: "OWNER_ID is required",
		},
		{
			name:    "missing message dump",
			setup:   func(c *Config) { c.MessageDump = 0 },
			wantErr: "MESSAGE_DUMP is required",
		},
		{
			name:    "missing sqlite path",
			setup:   func(c *Config) { c.SQLitePath = "" },
			wantErr: "SQLITE_PATH is required",
		},

		{
			name: "webhook requires domain",
			setup: func(c *Config) {
				c.UseWebhooks = true
				c.WebhookDomain = ""
				c.WebhookSecret = "secret"
			},
			wantErr: "WEBHOOK_DOMAIN is required",
		},
		{
			name: "webhook requires secret",
			setup: func(c *Config) {
				c.UseWebhooks = true
				c.WebhookDomain = "https://example.com"
				c.WebhookSecret = ""
			},
			wantErr: "WEBHOOK_SECRET is required",
		},
		{
			name:    "invalid HTTP port",
			setup:   func(c *Config) { c.HTTPPort = 70000 },
			wantErr: "HTTP_PORT must be between 1 and 65535",
		},
		{
			name:    "invalid dispatcher routines",
			setup:   func(c *Config) { c.DispatcherMaxRoutines = 1001 },
			wantErr: "DISPATCHER_MAX_ROUTINES must be between 1 and 1000",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validBaseConfig()
			cfg.DispatcherMaxRoutines = 200
			tc.setup(cfg)

			err := ValidateConfig(cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateConfig() error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}


