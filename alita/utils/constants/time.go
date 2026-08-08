package constants

import "time"

// Common time durations used throughout the application
const (
	// Cache durations
	AdminCacheTTL           = 30 * time.Minute
	RestrictedCacheTTL      = 30 * time.Minute
	RestrictedProbeInterval = 5 * time.Minute
	ShortCacheTTL           = 1 * time.Minute

	// Update intervals
	UserUpdateInterval    = 5 * time.Minute
	ChatUpdateInterval    = 5 * time.Minute
	ChannelUpdateInterval = 5 * time.Minute

	// Timeout durations
	DefaultTimeout  = 10 * time.Second
	ShortTimeout    = 5 * time.Second
	LongTimeout     = 30 * time.Second
	VeryLongTimeout = 120 * time.Second

	// HTTP server and Telegram API connection pooling
	DefaultHTTPPort         = 8080
	MaxIdleConns            = 100
	MaxIdleConnsPerHost     = 50
	MaxIdleConnsExtraBuffer = 20

	// DispatcherMaxRoutines bounds the concurrent update handlers.
	DispatcherMaxRoutines = 200

	// DefaultSQLitePath is the production database location (Docker volume).
	DefaultSQLitePath = "/data/alita.db"

	// InactivityThresholdDays is how long a chat may stay silent before it is
	// treated as inactive.
	InactivityThresholdDays = 30
)
