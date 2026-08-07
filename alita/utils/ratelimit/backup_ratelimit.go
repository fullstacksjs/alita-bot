package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/state"
)

// BackupRateLimiter provides rate limiting for backup operations
type BackupRateLimiter struct {
	mu sync.RWMutex
}

var (
	// Singleton instance
	backupLimiter *BackupRateLimiter
	once          = &sync.Once{}
)

// GetBackupRateLimiter returns the singleton rate limiter instance
func GetBackupRateLimiter() *BackupRateLimiter {
	once.Do(func() {
		backupLimiter = &BackupRateLimiter{}
	})
	return backupLimiter
}

// Cache key prefixes for rate limiting
const (
	exportRatePrefix = "backup:export:"
	importRatePrefix = "backup:import:"
	resetRatePrefix  = "backup:reset:"
)

// Default cooldown periods
const (
	DefaultExportCooldown = 5 * time.Minute
	DefaultImportCooldown = 10 * time.Minute
	DefaultResetCooldown  = 1 * time.Hour
)

// CanExport checks if an export operation is allowed for the given chat
// Returns true if allowed, and remaining cooldown if not
func (r *BackupRateLimiter) CanExport(chatID int64) (bool, time.Duration) {
	return r.canOperate(exportRatePrefix+strconv.FormatInt(chatID, 10), DefaultExportCooldown)
}

// RecordExport records an export operation for rate limiting
func (r *BackupRateLimiter) RecordExport(chatID int64) {
	cacheKey := exportRatePrefix + strconv.FormatInt(chatID, 10)
	r.recordOperation(cacheKey, DefaultExportCooldown)
}

// AcquireExport atomically reserves the export cooldown for a chat.
func (r *BackupRateLimiter) AcquireExport(chatID int64) (bool, time.Duration) {
	return r.acquireOperation(exportRatePrefix+strconv.FormatInt(chatID, 10), DefaultExportCooldown)
}

// CanImport checks if an import operation is allowed for the given chat
func (r *BackupRateLimiter) CanImport(chatID int64) (bool, time.Duration) {
	return r.canOperate(importRatePrefix+strconv.FormatInt(chatID, 10), DefaultImportCooldown)
}

// RecordImport records an import operation for rate limiting
func (r *BackupRateLimiter) RecordImport(chatID int64) {
	cacheKey := importRatePrefix + strconv.FormatInt(chatID, 10)
	r.recordOperation(cacheKey, DefaultImportCooldown)
}

// AcquireImport atomically reserves the import cooldown for a chat.
func (r *BackupRateLimiter) AcquireImport(chatID int64) (bool, time.Duration) {
	return r.acquireOperation(importRatePrefix+strconv.FormatInt(chatID, 10), DefaultImportCooldown)
}

// CanReset checks if a reset operation is allowed for the given chat
func (r *BackupRateLimiter) CanReset(chatID int64) (bool, time.Duration) {
	return r.canOperate(resetRatePrefix+strconv.FormatInt(chatID, 10), DefaultResetCooldown)
}

// RecordReset records a reset operation for rate limiting
func (r *BackupRateLimiter) RecordReset(chatID int64) {
	cacheKey := resetRatePrefix + strconv.FormatInt(chatID, 10)
	r.recordOperation(cacheKey, DefaultResetCooldown)
}

// AcquireReset atomically reserves the reset cooldown for a chat.
func (r *BackupRateLimiter) AcquireReset(chatID int64) (bool, time.Duration) {
	return r.acquireOperation(resetRatePrefix+strconv.FormatInt(chatID, 10), DefaultResetCooldown)
}

func (r *BackupRateLimiter) canOperate(cacheKey string, cooldown time.Duration) (bool, time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lastOperation, ok := state.Get[time.Time](context.Background(), cacheKey)
	if !ok {
		return true, 0
	}
	elapsed := time.Since(lastOperation)
	if elapsed >= cooldown {
		return true, 0
	}
	return false, cooldown - elapsed
}

func (r *BackupRateLimiter) acquireOperation(cacheKey string, cooldown time.Duration) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if lastOperation, ok := state.Get[time.Time](context.Background(), cacheKey); ok {
		if elapsed := now.Sub(lastOperation); elapsed < cooldown {
			return false, cooldown - elapsed
		}
	}
	state.Set(context.Background(), cacheKey, now, cooldown)
	return true, 0
}

// getLastOperation retrieves the timestamp of the last operation from cache
func (r *BackupRateLimiter) getLastOperation(cacheKey string) (time.Time, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ts, ok := state.Get[time.Time](context.Background(), cacheKey)
	if !ok {
		return time.Time{}, fmt.Errorf("no record found")
	}
	return ts, nil
}

// recordOperation stores the current timestamp in cache
func (r *BackupRateLimiter) recordOperation(cacheKey string, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state.Set(context.Background(), cacheKey, time.Now(), ttl)
}

// FormatCooldown formats a duration as a human-readable string
func FormatCooldown(duration time.Duration) string {
	if duration < time.Minute {
		seconds := int(duration.Seconds())
		return fmt.Sprintf("%d second%s", seconds, pluralSuffix(seconds))
	}
	if duration < time.Hour {
		minutes := int(duration.Minutes())
		seconds := int(duration.Seconds()) % 60
		if seconds > 0 {
			return fmt.Sprintf("%d minute%s %d second%s", minutes, pluralSuffix(minutes), seconds, pluralSuffix(seconds))
		}
		return fmt.Sprintf("%d minute%s", minutes, pluralSuffix(minutes))
	}
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%d hour%s %d minute%s", hours, pluralSuffix(hours), minutes, pluralSuffix(minutes))
	}
	return fmt.Sprintf("%d hour%s", hours, pluralSuffix(hours))
}

func pluralSuffix(value int) string {
	if value == 1 {
		return ""
	}
	return "s"
}
