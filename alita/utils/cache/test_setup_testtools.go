package cache

import (
	"context"
	"testing"

	"github.com/divkix/Alita_Robot/alita/utils/constants"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

// SetAdminCacheForTest seeds the in-process administrator cache for a chat and
// removes the entry when the test finishes. Tests use it to exercise cache-hit
// paths without going through the Telegram API.
func SetAdminCacheForTest(t *testing.T, chatId int64, adminCache AdminCache) {
	t.Helper()

	state.Set(context.Background(), adminCacheKey(chatId), adminCache, constants.AdminCacheTTL)
	t.Cleanup(func() {
		InvalidateAdminCache(chatId)
	})
}
