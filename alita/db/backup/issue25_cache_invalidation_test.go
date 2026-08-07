package backup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbcache "github.com/divkix/Alita_Robot/alita/db/cache"
	"github.com/divkix/Alita_Robot/alita/db/chats"
	"github.com/divkix/Alita_Robot/alita/db/filters"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

// Backup mutations must drop the retained-domain keys from the in-process
// state store, otherwise a chat keeps serving the pre-import snapshot.
func TestIssue25_BackupMutationsInvalidateRetainedDomainState(t *testing.T) {
	skipIfNoDb(t)
	state.SimulateRestart()

	chatID := time.Now().UnixNano()
	require.NoError(t, chats.EnsureChatInDb(chatID, "issue25_backup_cache"))
	t.Cleanup(func() { cleanupBackupChat(t, chatID) })

	require.NoError(t, filters.AddFilter(chatID, "before", "old reply", "", nil, 1))

	// Warm the retained filter caches through the normal read path.
	names := filters.GetFiltersList(chatID)
	require.Equal(t, []string{"before"}, names)
	_, err := filters.GetChatFiltersCached(chatID)
	require.NoError(t, err)

	listKey := dbcache.CacheKey("filter_list", chatID)
	optimizedKey := dbcache.CacheKey("filters_optimized", chatID)
	if _, ok := state.Get[[]string](context.Background(), listKey); !ok {
		t.Fatalf("filter list was not cached in the in-process store under %s", listKey)
	}

	payload := map[string]interface{}{
		"filters": []map[string]interface{}{
			{"keyword": "after", "filter_reply": "new reply", "msgtype": 1},
		},
	}
	require.NoError(t, ImportDomainData(chatID, DomainFilters, payload))

	if cached, ok := state.Get[[]string](context.Background(), listKey); ok {
		t.Fatalf("filter list key still cached after import: %v", cached)
	}
	if _, ok := state.Get[any](context.Background(), optimizedKey); ok {
		t.Fatal("optimized filter key still cached after import")
	}

	names = filters.GetFiltersList(chatID)
	require.Equal(t, []string{"after"}, names)

	// Clearing the domain must invalidate the freshly warmed keys as well.
	require.NoError(t, ClearDomainData(chatID, DomainFilters))
	if cached, ok := state.Get[[]string](context.Background(), listKey); ok {
		t.Fatalf("filter list key still cached after clear: %v", cached)
	}

	names = filters.GetFiltersList(chatID)
	require.Empty(t, names)
}
