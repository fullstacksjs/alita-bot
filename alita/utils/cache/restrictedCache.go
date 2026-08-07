package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/divkix/Alita_Robot/alita/utils/constants"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

var (
	restrictedCacheHits   atomic.Int64
	restrictedCacheMisses atomic.Int64
)

// restrictedChatKey returns the key for a restricted chat.
func restrictedChatKey(chatID int64) string {
	return fmt.Sprintf("alita:restricted:%d", chatID)
}

// restrictedProbeKey returns the key used to coordinate probe attempts.
func restrictedProbeKey(chatID int64) string {
	return fmt.Sprintf("alita:restricted_probe:%d", chatID)
}

// MarkChatRestricted marks a chat as restricted (bot can't send messages).
// The restriction expires after RestrictedCacheTTL (30 min).
func MarkChatRestricted(chatID int64) {
	state.Set(
		context.Background(),
		restrictedChatKey(chatID),
		time.Now().Format(time.RFC3339),
		constants.RestrictedCacheTTL,
	)
	log.WithField("chat_id", chatID).Info("[RestrictedCache] Marked chat as restricted")
}

// IsChatRestricted checks if a chat is currently in the restricted cache.
// Returns true if the bot should skip sending to this chat.
// A periodic probe window allows retries so stale restrictions don't block sends
// for the full key TTL.
func IsChatRestricted(chatID int64) bool {
	ts, ok := state.Get[string](context.Background(), restrictedChatKey(chatID))
	if !ok {
		restrictedCacheMisses.Add(1)
		return false
	}

	restrictedSince, parseErr := time.Parse(time.RFC3339, ts)
	if parseErr != nil {
		// Allow a probe when cache payload is malformed to avoid hard lockout.
		restrictedCacheMisses.Add(1)
		log.WithFields(log.Fields{
			"chat_id": chatID,
			"value":   ts,
			"error":   parseErr,
		}).Debug("[RestrictedCache] Invalid timestamp, allowing send probe")
		return false
	}

	if time.Since(restrictedSince) >= constants.RestrictedProbeInterval {
		// Coordinate a single probe attempt across concurrent workers so only one
		// sender retries Telegram when probe window opens.
		probeKey := restrictedProbeKey(chatID)
		if _, probeActive := state.Get[string](context.Background(), probeKey); probeActive {
			restrictedCacheHits.Add(1)
			log.WithFields(log.Fields{
				"chat_id": chatID,
				"since":   ts,
			}).Debug("[RestrictedCache] Probe already in progress, skipping send")
			return true
		}

		state.Set(context.Background(), probeKey, time.Now().Format(time.RFC3339), constants.ShortCacheTTL)
		restrictedCacheMisses.Add(1)
		log.WithFields(log.Fields{
			"chat_id": chatID,
			"since":   ts,
		}).Debug("[RestrictedCache] Probe window reached, allowing send attempt")
		return false
	}

	restrictedCacheHits.Add(1)
	log.WithField("chat_id", chatID).Debugf("[RestrictedCache] Cache hit — skipping send to restricted chat (since %s)", ts)
	return true
}

// MarkChatNotRestricted removes the restricted flag for a chat.
// Called when the bot's permissions are upgraded (e.g., admin cache load detects bot is admin).
func MarkChatNotRestricted(chatID int64) {
	state.Delete(context.Background(), restrictedChatKey(chatID))
	state.Delete(context.Background(), restrictedProbeKey(chatID))
	log.WithField("chat_id", chatID).Info("[RestrictedCache] Cleared restricted flag — bot can now send")
}

// GetRestrictedCacheStats returns cumulative hit/miss counters for monitoring.
func GetRestrictedCacheStats() (hits, misses int64) {
	return restrictedCacheHits.Load(), restrictedCacheMisses.Load()
}
