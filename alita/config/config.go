package config

import (
	"fmt"
	"os"
	"path"
	"runtime"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/utils/logredact"
)

// isCliModeActive returns true if the program is running with CLI flags
// that should skip database initialization (--version, --health, -v).
// This allows init() functions to return early without requiring DB connection.
func isCliModeActive() bool {
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

func getHTTPPort() int {
	value := os.Getenv("HTTP_PORT")
	if value == "" {
		value = os.Getenv("PORT")
	}
	return typeConvertor{str: value}.Int()
}

// Config holds all configuration for the bot
type Config struct {
	// Core configuration
	BotToken    string `validate:"required"`
	BotVersion  string
	ApiServer   string
	WorkingMode string
	Debug       bool

	// Bot settings
	OwnerId            int64 `validate:"required,min=1"`
	MessageDump        int64 `validate:"required,min=1"`
	DropPendingUpdates bool
	AllowedUpdates     []string

	// Database configuration (SQLite only)
	SQLitePath string

	// Database monitoring configuration
	EnableDBMonitoring bool `env:"ENABLE_DB_MONITORING" envDefault:"false"`

	// HTTP Server configuration (unified server for health, metrics, webhook)
	HTTPPort int `validate:"min=1,max=65535"`

	// Webhook configuration
	UseWebhooks   bool
	WebhookDomain string
	WebhookSecret string

	// Safety and performance limits
	EnablePerformanceMonitoring bool
	EnableBackgroundStats       bool
	DispatcherMaxRoutines       int `validate:"min=1,max=1000"` // Max concurrent goroutines for dispatcher

	// Activity monitoring configuration
	InactivityThresholdDays int  `validate:"min=1,max=365"` // Days before marking a chat as inactive
	ActivityCheckInterval   int  `validate:"min=1,max=24"`  // Hours between activity checks
	EnableAutoCleanup       bool // Whether to automatically mark inactive chats

	// Performance optimization settings
	HTTPMaxIdleConns        int `validate:"min=10,max=1000"` // HTTP connection pool size
	HTTPMaxIdleConnsPerHost int `validate:"min=5,max=500"`   // HTTP connections per host

	// Resource monitoring limits
	ResourceMaxGoroutines int `validate:"min=100,max=10000"` // Maximum goroutines before triggering cleanup
	ResourceMaxMemoryMB   int `validate:"min=100,max=10000"` // Maximum memory usage in MB
	ResourceGCThresholdMB int `validate:"min=100,max=5000"`  // Memory threshold for triggering GC

	// Profiling configuration
	EnablePPROF bool // Enable pprof endpoints for performance profiling (development only)

	// Metrics authentication
	MetricsAuthToken string // Bearer token required to access /metrics and /db_metrics (empty = unauthenticated with a warning)
}

// AppConfig is the global configuration instance - the single source of truth.
// All code should access configuration via config.AppConfig.FieldName
var AppConfig *Config

// ValidateConfig validates the configuration struct and returns an error if any required
// fields are missing or values are outside acceptable ranges.
func ValidateConfig(cfg *Config) error {
	if cfg.BotToken == "" {
		return fmt.Errorf("BOT_TOKEN is required")
	}
	if cfg.OwnerId == 0 {
		return fmt.Errorf("OWNER_ID is required and must be greater than 0")
	}
	if cfg.MessageDump == 0 {
		return fmt.Errorf("MESSAGE_DUMP is required and must be greater than 0")
	}
	if cfg.SQLitePath == "" {
		return fmt.Errorf("SQLITE_PATH is required")
	}

	// Validate webhook configuration if webhooks are enabled
	if cfg.UseWebhooks {
		if cfg.WebhookDomain == "" {
			return fmt.Errorf("WEBHOOK_DOMAIN is required when USE_WEBHOOKS is enabled")
		}
		if cfg.WebhookSecret == "" {
			return fmt.Errorf("WEBHOOK_SECRET is required when USE_WEBHOOKS is enabled for security")
		}
	}

	// Validate HTTP port
	if cfg.HTTPPort <= 0 || cfg.HTTPPort > 65535 {
		return fmt.Errorf("HTTP_PORT must be between 1 and 65535")
	}

	// Validate performance limits
	if cfg.DispatcherMaxRoutines != 0 && (cfg.DispatcherMaxRoutines < 1 || cfg.DispatcherMaxRoutines > 1000) {
		return fmt.Errorf("DISPATCHER_MAX_ROUTINES must be between 1 and 1000")
	}

	return nil
}

// LoadConfig loads configuration from environment variables, applies defaults,
// validates the configuration, and returns a populated Config instance.
func LoadConfig() (*Config, error) {
	// load goenv config
	_ = godotenv.Load() // Ignore error as .env file is optional

	cfg := &Config{
		// Core configuration
		BotToken:    os.Getenv("BOT_TOKEN"),
		BotVersion:  "3.4.0",
		ApiServer:   os.Getenv("API_SERVER"),
		WorkingMode: "worker",
		Debug:       typeConvertor{str: os.Getenv("DEBUG")}.Bool(),

		// Bot settings
		OwnerId:            typeConvertor{str: os.Getenv("OWNER_ID")}.Int64(),
		MessageDump:        typeConvertor{str: os.Getenv("MESSAGE_DUMP")}.Int64(),
		DropPendingUpdates: typeConvertor{str: os.Getenv("DROP_PENDING_UPDATES")}.Bool(),

		// Database configuration
		SQLitePath: os.Getenv("SQLITE_PATH"),

		// Database monitoring configuration
		EnableDBMonitoring: typeConvertor{str: os.Getenv("ENABLE_DB_MONITORING")}.Bool(),

		// HTTP Server configuration
		HTTPPort: getHTTPPort(),

		// Webhook configuration
		UseWebhooks:   typeConvertor{str: os.Getenv("USE_WEBHOOKS")}.Bool(),
		WebhookDomain: os.Getenv("WEBHOOK_DOMAIN"),
		WebhookSecret: os.Getenv("WEBHOOK_SECRET"),

		// Safety and performance limits
		EnablePerformanceMonitoring: typeConvertor{str: os.Getenv("ENABLE_PERFORMANCE_MONITORING")}.Bool(),
		EnableBackgroundStats:       typeConvertor{str: os.Getenv("ENABLE_BACKGROUND_STATS")}.Bool(),
		DispatcherMaxRoutines:       typeConvertor{str: os.Getenv("DISPATCHER_MAX_ROUTINES")}.Int(),

		// Activity monitoring configuration
		InactivityThresholdDays: typeConvertor{str: os.Getenv("INACTIVITY_THRESHOLD_DAYS")}.Int(),
		ActivityCheckInterval:   typeConvertor{str: os.Getenv("ACTIVITY_CHECK_INTERVAL")}.Int(),
		EnableAutoCleanup:       typeConvertor{str: os.Getenv("ENABLE_AUTO_CLEANUP")}.Bool(),

		// Performance optimization settings
		HTTPMaxIdleConns:        typeConvertor{str: os.Getenv("HTTP_MAX_IDLE_CONNS")}.Int(),
		HTTPMaxIdleConnsPerHost: typeConvertor{str: os.Getenv("HTTP_MAX_IDLE_CONNS_PER_HOST")}.Int(),

		// Resource monitoring limits
		ResourceMaxGoroutines: typeConvertor{str: os.Getenv("RESOURCE_MAX_GOROUTINES")}.Int(),
		ResourceMaxMemoryMB:   typeConvertor{str: os.Getenv("RESOURCE_MAX_MEMORY_MB")}.Int(),
		ResourceGCThresholdMB: typeConvertor{str: os.Getenv("RESOURCE_GC_THRESHOLD_MB")}.Int(),

		// Profiling configuration
		EnablePPROF: typeConvertor{str: os.Getenv("ENABLE_PPROF")}.Bool(),

		// Metrics authentication
		MetricsAuthToken: os.Getenv("METRICS_AUTH_TOKEN"),
	}

	// Set defaults
	cfg.setDefaults()

	// Validate configuration
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Set allowed updates
	cfg.AllowedUpdates = []string{
		"message",
		"edited_message",
		"channel_post",
		"edited_channel_post",
		"inline_query",
		"chosen_inline_result",
		"callback_query",
		"shipping_query",
		"pre_checkout_query",
		"poll",
		"poll_answer",
		"my_chat_member",
		"chat_member",
		"chat_join_request",
	}

	return cfg, nil
}

// setDefaults sets default values for configuration fields that are not provided
// via environment variables. It calculates appropriate defaults based on system
// resources and production best practices.
func (cfg *Config) setDefaults() {
	if cfg.ApiServer == "" {
		cfg.ApiServer = "https://api.telegram.org"
	}
	if cfg.WorkingMode == "" {
		cfg.WorkingMode = "worker"
	}

	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}

	// Set activity monitoring defaults
	if cfg.InactivityThresholdDays == 0 {
		cfg.InactivityThresholdDays = 30 // 30 days before marking as inactive
	}
	if cfg.ActivityCheckInterval == 0 {
		cfg.ActivityCheckInterval = 1 // Check every hour
	}
	// EnableAutoCleanup defaults to true unless explicitly set to false
	if os.Getenv("ENABLE_AUTO_CLEANUP") == "" {
		cfg.EnableAutoCleanup = true
	}

	// Set default safety limits
	if cfg.DispatcherMaxRoutines == 0 {
		cfg.DispatcherMaxRoutines = 200 // Optimized for better throughput
	}

	// Enable monitoring by default in production
	if !cfg.Debug {
		if os.Getenv("ENABLE_PERFORMANCE_MONITORING") == "" {
			cfg.EnablePerformanceMonitoring = true
		}
		if os.Getenv("ENABLE_BACKGROUND_STATS") == "" {
			cfg.EnableBackgroundStats = true
		}
	}

	if cfg.SQLitePath == "" {
		cfg.SQLitePath = "/data/alita.db"
	}

	// Set performance optimization defaults (enabled by default for better performance)
	if cfg.HTTPMaxIdleConns == 0 {
		cfg.HTTPMaxIdleConns = 100
	}
	if cfg.HTTPMaxIdleConnsPerHost == 0 {
		cfg.HTTPMaxIdleConnsPerHost = 50
	}

	// Set resource monitoring defaults
	if cfg.ResourceMaxGoroutines == 0 {
		cfg.ResourceMaxGoroutines = 1000
	}
	if cfg.ResourceMaxMemoryMB == 0 {
		cfg.ResourceMaxMemoryMB = 500
	}
	if cfg.ResourceGCThresholdMB == 0 {
		cfg.ResourceGCThresholdMB = 400
	}
}

// init initializes the logging configuration, loads the global configuration
// from environment variables, validates it, and sets up global variables for
// backward compatibility. This function is called automatically at package import.
func init() {
	// Skip config validation when running in CLI mode (--version, --health)
	// This allows these flags to work without requiring valid configuration.
	if isCliModeActive() {
		AppConfig = &Config{}
		return
	}

	// set logger config
	log.SetLevel(log.DebugLevel)
	// SetReportCaller will be configured after debug mode is determined
	log.SetFormatter(
		&log.JSONFormatter{
			DisableHTMLEscape: true,
			PrettyPrint:       false,
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				return f.Function, fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
			},
		},
	)

	// Install the sensitive-data redaction hook before any configuration is
	// loaded. Structural patterns (bot tokens, DSN passwords, bearer tokens)
	// are scrubbed immediately; exact secrets are registered below once known.
	logredact.Install(nil)

	// Load the structured configuration
	cfg, err := LoadConfig()
	if err != nil {
		// If essential env vars are missing (e.g., during unit tests), provide zero-value config
		if os.Getenv("BOT_TOKEN") == "" {
			AppConfig = &Config{}
			return
		}
		log.Fatalf("[Config] Failed to load configuration: %v", err)
	}

	// Set global configuration instance
	AppConfig = cfg

	// Register the now-known exact secrets so they are scrubbed from any log
	// line that happens to include them verbatim (e.g. a wrapped DB error
	// containing the DSN, or a startup dump).
	logredact.RegisterSecret(
		cfg.BotToken,
		cfg.WebhookSecret,
		cfg.MetricsAuthToken,
	)

	// Configure logger based on debug mode
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	log.SetReportCaller(cfg.Debug) // Only enable stack traces in debug mode

	log.Info("[Config] Configuration loaded and validated successfully")
}
