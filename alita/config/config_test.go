package config

import (
	"os"
	"testing"
)

func skipIfNoConfig(t *testing.T) {
	t.Helper()
	if os.Getenv("BOT_TOKEN") == "" {
		t.Skip("skipping: BOT_TOKEN not set (config.init() would fatalf)")
	}
}

func validBaseConfig() *Config {
	return &Config{
		BotToken:    "test-token",
		OwnerId:     1,
		MessageDump: 1,
		DatabaseURL: "postgres://localhost/test",
		HTTPPort:    8080,
	}
}

func TestValidateConfig(t *testing.T) {
	t.Parallel()
	skipIfNoConfig(t)

	tests := []struct {
		name    string
		setup   func(*Config)
		wantErr bool
	}{
		// Required field validations
		{
			name:    "valid base config succeeds",
			setup:   func(_ *Config) {},
			wantErr: false,
		},
		{
			name:    "empty BotToken returns error",
			setup:   func(c *Config) { c.BotToken = "" },
			wantErr: true,
		},
		{
			name:    "OwnerId zero returns error",
			setup:   func(c *Config) { c.OwnerId = 0 },
			wantErr: true,
		},
		{
			name:    "MessageDump zero returns error",
			setup:   func(c *Config) { c.MessageDump = 0 },
			wantErr: true,
		},
		{
			name:    "empty DatabaseURL returns error",
			setup:   func(c *Config) { c.DatabaseURL = "" },
			wantErr: true,
		},

		// Webhook validations
		{
			name: "UseWebhooks with empty domain returns error",
			setup: func(c *Config) {
				c.UseWebhooks = true
				c.WebhookDomain = ""
				c.WebhookSecret = "secret"
			},
			wantErr: true,
		},
		{
			name: "UseWebhooks with empty secret returns error",
			setup: func(c *Config) {
				c.UseWebhooks = true
				c.WebhookDomain = "example.com"
				c.WebhookSecret = ""
			},
			wantErr: true,
		},
		{
			name: "UseWebhooks false with no domain succeeds",
			setup: func(c *Config) {
				c.UseWebhooks = false
				c.WebhookDomain = ""
				c.WebhookSecret = ""
			},
			wantErr: false,
		},
		{
			name: "UseWebhooks true with both domain and secret succeeds",
			setup: func(c *Config) {
				c.UseWebhooks = true
				c.WebhookDomain = "example.com"
				c.WebhookSecret = "mysecret"
			},
			wantErr: false,
		},
		// HTTP port validations
		{
			name:    "HTTPPort zero returns error",
			setup:   func(c *Config) { c.HTTPPort = 0 },
			wantErr: true,
		},
		{
			name:    "HTTPPort 70000 returns error",
			setup:   func(c *Config) { c.HTTPPort = 70000 },
			wantErr: true,
		},
		{
			name:    "HTTPPort 65535 succeeds",
			setup:   func(c *Config) { c.HTTPPort = 65535 },
			wantErr: false,
		},
		{
			name:    "HTTPPort 1 succeeds",
			setup:   func(c *Config) { c.HTTPPort = 1 },
			wantErr: false,
		},
		// Dispatcher optional field validation
		{
			name:    "DispatcherMaxRoutines zero is allowed",
			setup:   func(c *Config) { c.DispatcherMaxRoutines = 0 },
			wantErr: false,
		},
		{
			name:    "DispatcherMaxRoutines 1 succeeds",
			setup:   func(c *Config) { c.DispatcherMaxRoutines = 1 },
			wantErr: false,
		},
		{
			name:    "DispatcherMaxRoutines 1000 succeeds",
			setup:   func(c *Config) { c.DispatcherMaxRoutines = 1000 },
			wantErr: false,
		},
		// DB pool optional field validation
		{
			name:    "DBMaxIdleConns 101 returns error",
			setup:   func(c *Config) { c.DBMaxIdleConns = 101 },
			wantErr: true,
		},
		{
			name:    "DBMaxIdleConns 0 is allowed (optional field)",
			setup:   func(c *Config) { c.DBMaxIdleConns = 0 },
			wantErr: false,
		},
		{
			name:    "DBMaxIdleConns 100 succeeds",
			setup:   func(c *Config) { c.DBMaxIdleConns = 100 },
			wantErr: false,
		},
		{
			name:    "DBMaxOpenConns 1001 returns error",
			setup:   func(c *Config) { c.DBMaxOpenConns = 1001 },
			wantErr: true,
		},
		{
			name:    "DBMaxOpenConns 0 is allowed (optional field)",
			setup:   func(c *Config) { c.DBMaxOpenConns = 0 },
			wantErr: false,
		},
		{
			name:    "DBMaxOpenConns 1000 succeeds",
			setup:   func(c *Config) { c.DBMaxOpenConns = 1000 },
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validBaseConfig()
			tc.setup(cfg)

			err := ValidateConfig(cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	t.Run("zero config gets defaults", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.setDefaults()

		if cfg.ApiServer != "https://api.telegram.org" {
			t.Errorf("ApiServer: got %q, want %q", cfg.ApiServer, "https://api.telegram.org")
		}
		if cfg.WorkingMode != "worker" {
			t.Errorf("WorkingMode: got %q, want %q", cfg.WorkingMode, "worker")
		}
		if cfg.HTTPPort != 8080 {
			t.Errorf("HTTPPort: got %d, want %d", cfg.HTTPPort, 8080)
		}
		if cfg.DBMaxIdleConns != 50 {
			t.Errorf("DBMaxIdleConns: got %d, want %d", cfg.DBMaxIdleConns, 50)
		}
		if cfg.DBMaxOpenConns != 200 {
			t.Errorf("DBMaxOpenConns: got %d, want %d", cfg.DBMaxOpenConns, 200)
		}
		if cfg.DBConnMaxLifetimeMin != 240 {
			t.Errorf("DBConnMaxLifetimeMin: got %d, want %d", cfg.DBConnMaxLifetimeMin, 240)
		}
		if cfg.DBConnMaxIdleTimeMin != 60 {
			t.Errorf("DBConnMaxIdleTimeMin: got %d, want %d", cfg.DBConnMaxIdleTimeMin, 60)
		}
		if cfg.MigrationsPath != "migrations" {
			t.Errorf("MigrationsPath: got %q, want %q", cfg.MigrationsPath, "migrations")
		}
		if cfg.DispatcherMaxRoutines != 200 {
			t.Errorf("DispatcherMaxRoutines: got %d, want %d", cfg.DispatcherMaxRoutines, 200)
		}
		if cfg.ResourceMaxGoroutines != 1000 {
			t.Errorf("ResourceMaxGoroutines: got %d, want %d", cfg.ResourceMaxGoroutines, 1000)
		}
		if cfg.ResourceMaxMemoryMB != 500 {
			t.Errorf("ResourceMaxMemoryMB: got %d, want %d", cfg.ResourceMaxMemoryMB, 500)
		}
		if cfg.ResourceGCThresholdMB != 400 {
			t.Errorf("ResourceGCThresholdMB: got %d, want %d", cfg.ResourceGCThresholdMB, 400)
		}
	})

	t.Run("pre-set ApiServer preserved", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{ApiServer: "custom"}
		cfg.setDefaults()

		if cfg.ApiServer != "custom" {
			t.Errorf("ApiServer: got %q, want %q", cfg.ApiServer, "custom")
		}
	})



	t.Run("pre-set HTTPPort preserved", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{HTTPPort: 3000}
		cfg.setDefaults()

		if cfg.HTTPPort != 3000 {
			t.Errorf("HTTPPort: got %d, want %d", cfg.HTTPPort, 3000)
		}
	})
}



func TestGetHTTPPort(t *testing.T) {
	t.Run("HTTP_PORT wins", func(t *testing.T) {
		t.Setenv("HTTP_PORT", "8081")
		t.Setenv("PORT", "9091")
		if got := getHTTPPort(); got != 8081 {
			t.Fatalf("getHTTPPort() = %d, want 8081", got)
		}
	})
	t.Run("Railway PORT fallback", func(t *testing.T) {
		t.Setenv("HTTP_PORT", "")
		t.Setenv("PORT", "9091")
		if got := getHTTPPort(); got != 9091 {
			t.Fatalf("getHTTPPort() = %d, want 9091", got)
		}
	})
}


