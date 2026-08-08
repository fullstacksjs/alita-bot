package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

	"github.com/PaulSonOfLars/gotgbot/v2"

	"github.com/divkix/Alita_Robot/alita/utils/constants"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

var (
	// adminLoadGroup collapses concurrent administrator loads for the same chat
	// so a burst of updates results in a single Telegram API round trip.
	adminLoadGroup singleflight.Group
	// adminCacheGeneration is bumped on every invalidation so an in-flight load
	// started before a promotion or demotion cannot store its stale snapshot.
	adminCacheGeneration atomic.Uint64
)

// adminCacheKey returns the in-process state key holding a chat's admin list.
func adminCacheKey(chatId int64) string {
	return fmt.Sprintf("alita:adminCache:%d", chatId)
}

// clone returns a copy of the admin cache so callers cannot mutate the shared
// in-process entry. Entries used to be re-created per read by deserialization.
func (a AdminCache) clone() AdminCache {
	cloned := AdminCache{
		ChatId: a.ChatId,
		Cached: a.Cached,
	}
	if a.UserInfo != nil {
		cloned.UserInfo = make([]gotgbot.MergedChatMember, len(a.UserInfo))
		copy(cloned.UserInfo, a.UserInfo)
	}
	if a.UserMap != nil {
		cloned.UserMap = make(map[int64]gotgbot.MergedChatMember, len(a.UserMap))
		for id, member := range a.UserMap {
			cloned.UserMap[id] = member
		}
	}
	return cloned
}

// LoadAdminCache retrieves and caches the list of administrators for a given chat.
// It fetches the current administrators from Telegram API and stores them in the
// in-process state store with the configured admin TTL. Returns an AdminCache
// struct containing the admin list.
func LoadAdminCache(b *gotgbot.Bot, chatId int64) AdminCache {
	if b == nil {
		log.Error("LoadAdminCache: bot is nil")
		return AdminCache{}
	}

	key := adminCacheKey(chatId)
	loaded, _, _ := adminLoadGroup.Do(key, func() (interface{}, error) {
		// A concurrent burst only shares one flight while that flight is running;
		// callers arriving just after it must reuse the entry it stored instead of
		// issuing another Telegram round trip. Callers that need fresh data
		// invalidate first, which drops the entry and bumps the generation.
		if cached, ok := state.Get[AdminCache](context.Background(), key); ok && cached.Cached {
			return cached, nil
		}
		return loadAdminCache(b, chatId), nil
	})

	adminCache, ok := loaded.(AdminCache)
	if !ok {
		return AdminCache{}
	}
	return adminCache.clone()
}

func loadAdminCache(b *gotgbot.Bot, chatId int64) AdminCache {
	generation := adminCacheGeneration.Load()
	storeResult := func(adminCache AdminCache) AdminCache {
		// Skip the write when an invalidation happened while this load ran, so a
		// pre-promotion snapshot cannot outlive the update that invalidated it.
		if generation != adminCacheGeneration.Load() {
			return adminCache
		}
		key := adminCacheKey(chatId)
		state.Set(context.Background(), key, adminCache, constants.AdminCacheTTL)
		if generation != adminCacheGeneration.Load() {
			state.Delete(context.Background(), key)
		}
		return adminCache
	}

	// Create context with timeout to prevent indefinite blocking
	ctx, cancel := context.WithTimeout(context.Background(), constants.DefaultTimeout)
	defer cancel()

	// First, check if bot itself is admin to diagnose permission issues
	botMember, botErr := b.GetChatMemberWithContext(ctx, chatId, b.Id, nil)
	if botErr != nil {
		log.WithFields(log.Fields{
			"chatId": chatId,
			"botId":  b.Id,
			"error":  botErr,
		}).Warning("LoadAdminCache: Could not verify bot admin status")
		// If we can't even check bot status, likely not admin - return empty cache
		return AdminCache{
			ChatId:   chatId,
			UserInfo: []gotgbot.MergedChatMember{},
			Cached:   true,
		}
	}

	botStatus := botMember.GetStatus()
	if botStatus != "administrator" && botStatus != "creator" {
		return storeResult(AdminCache{
			ChatId:   chatId,
			UserInfo: []gotgbot.MergedChatMember{},
			Cached:   true,
		})
	}

	// Bot has admin rights — clear any stale restricted flag so sends are
	// no longer short-circuited for this chat.
	MarkChatNotRestricted(chatId)

	log.WithFields(log.Fields{
		"chatId":    chatId,
		"botId":     b.Id,
		"botStatus": botStatus,
	}).Debug("LoadAdminCache: Bot has admin privileges")

	// Retry logic for API call
	maxRetries := 3
	var adminList []gotgbot.ChatMember
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		adminList, err = b.GetChatAdministratorsWithContext(ctx, chatId, nil)
		if err != nil {
			log.WithFields(log.Fields{
				"chatId":    chatId,
				"error":     err,
				"attempt":   attempt + 1,
				"errorType": fmt.Sprintf("%T", err),
			}).Warning("LoadAdminCache: Failed to get chat administrators, retrying...")

			if attempt < maxRetries-1 {
				time.Sleep(time.Duration(attempt+1) * time.Second) // Exponential backoff
				continue
			}

			log.WithFields(log.Fields{
				"chatId": chatId,
				"error":  err,
			}).Error("LoadAdminCache: Failed to get chat administrators after all retries")
			return AdminCache{}
		}
		break // Success
	}

	if len(adminList) == 0 {
		log.WithFields(log.Fields{
			"chatId": chatId,
		}).Warning("LoadAdminCache: No administrators found - this is unusual for a valid group")
		// Empty admin list is unusual but not necessarily an error
		// Return empty cache but mark it as cached to avoid infinite retries
		return storeResult(AdminCache{
			ChatId:   chatId,
			UserInfo: []gotgbot.MergedChatMember{},
			Cached:   true,
		})
	}

	// Convert ChatMember to MergedChatMember and build lookup map
	userList := make([]gotgbot.MergedChatMember, 0, len(adminList))
	userMap := make(map[int64]gotgbot.MergedChatMember, len(adminList))
	for _, admin := range adminList {
		merged := admin.MergeChatMember()
		userList = append(userList, merged)
		// GetUser returns User by value, so check if ID is valid (non-zero)
		user := admin.GetUser()
		if user.Id != 0 {
			userMap[user.Id] = merged
		}
	}

	adminCache := AdminCache{
		ChatId:   chatId,
		UserInfo: userList,
		UserMap:  userMap,
		Cached:   true,
	}

	return storeResult(adminCache)
}

// GetAdminCacheList retrieves the cached administrator list for a specific chat.
// Returns true and the AdminCache if found in cache, false and empty AdminCache if cache miss.
func GetAdminCacheList(chatId int64) (bool, AdminCache) {
	adminCache, ok := state.Get[AdminCache](context.Background(), adminCacheKey(chatId))
	if !ok {
		log.WithFields(log.Fields{
			"chatId": chatId,
		}).Debug("GetAdminCacheList: Cache miss, will attempt fallback")
		return false, AdminCache{}
	}
	return true, adminCache.clone()
}

// GetAdminCacheUser searches for a specific user in the cached administrator list of a chat.
// Returns true and the MergedChatMember if the user is found as an admin,
// false and empty MergedChatMember if not found or cache miss.
func GetAdminCacheUser(chatId, userId int64) (bool, gotgbot.MergedChatMember) {
	adminCache, ok := state.Get[AdminCache](context.Background(), adminCacheKey(chatId))
	if !ok {
		return false, gotgbot.MergedChatMember{}
	}

	// O(1) lookup via map (primary method)
	if admin, found := adminCache.UserMap[userId]; found {
		return true, admin
	}

	// Fallback to linear search for entries stored without a lookup map
	for i := range adminCache.UserInfo {
		admin := &adminCache.UserInfo[i]
		if admin.User.Id == userId {
			return true, *admin
		}
	}
	return false, gotgbot.MergedChatMember{}
}

// InvalidateAdminCache removes the cached admin list for a chat.
// Should be called when admins are promoted/demoted to ensure fresh data.
func InvalidateAdminCache(chatId int64) {
	// Bump the generation and drop any in-flight singleflight result before the
	// delete so the next load cannot join a load that predates this invalidation.
	adminCacheGeneration.Add(1)
	adminLoadGroup.Forget(adminCacheKey(chatId))

	state.Delete(context.Background(), adminCacheKey(chatId))
	log.Debugf("[AdminCache] Invalidated admin cache for chat %d", chatId)
}
