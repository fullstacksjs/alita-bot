package db

import (
	"strings"
	"time"
)

// sqliteRetryAttempts bounds how often a locked SQLite write is retried.
const sqliteRetryAttempts = 10

// RetryOnLock runs fn and retries it while SQLite reports a busy or locked
// database. SQLite serializes writers, so concurrent setting changes surface as
// transient "database is locked" errors rather than lost updates. Every other
// error, and every dialect other than SQLite, returns immediately.
func RetryOnLock(fn func() error) error {
	var err error
	for attempt := 0; attempt < sqliteRetryAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isSQLiteLockError(err) {
			break
		}
		time.Sleep(time.Duration(25*(attempt+1)) * time.Millisecond)
	}
	return err
}

// isSQLiteLockError reports whether err is a transient SQLite contention error.
func isSQLiteLockError(err error) bool {
	if DB == nil || DB.Dialector == nil || DB.Name() != "sqlite" {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "locked") || strings.Contains(msg, "busy")
}
