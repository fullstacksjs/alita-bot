package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/divkix/Alita_Robot/alita/utils/constants"
	"github.com/divkix/Alita_Robot/alita/utils/state"
)

// ---------------------------------------------------------------------------
// restrictedChatKey
// ---------------------------------------------------------------------------

func TestRestrictedCacheKey_Format(t *testing.T) {
	t.Parallel()

	cases := []struct {
		chatID   int64
		expected string
	}{
		{-1001618764357, "alita:restricted:-1001618764357"},
		{123456789, "alita:restricted:123456789"},
		{0, "alita:restricted:0"},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("chatID=%d", tc.chatID), func(t *testing.T) {
			t.Parallel()
			got := restrictedChatKey(tc.chatID)
			if got != tc.expected {
				t.Errorf("restrictedChatKey(%d) = %q, want %q", tc.chatID, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// restrictedProbeKey
// ---------------------------------------------------------------------------

func TestRestrictedProbeKey_Format(t *testing.T) {
	t.Parallel()

	cases := []struct {
		chatID   int64
		expected string
	}{
		{-1001618764357, "alita:restricted_probe:-1001618764357"},
		{123456789, "alita:restricted_probe:123456789"},
		{0, "alita:restricted_probe:0"},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("chatID=%d", tc.chatID), func(t *testing.T) {
			t.Parallel()
			got := restrictedProbeKey(tc.chatID)
			if got != tc.expected {
				t.Errorf("restrictedProbeKey(%d) = %q, want %q", tc.chatID, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetRestrictedCacheStats (atomic counters)
// ---------------------------------------------------------------------------

func TestGetRestrictedCacheStats_InitialZero(t *testing.T) {
	origHits := restrictedCacheHits.Load()
	origMisses := restrictedCacheMisses.Load()
	defer func() {
		restrictedCacheHits.Store(origHits)
		restrictedCacheMisses.Store(origMisses)
	}()

	restrictedCacheHits.Store(0)
	restrictedCacheMisses.Store(0)

	hits, misses := GetRestrictedCacheStats()
	if hits != 0 {
		t.Errorf("expected initial hits = 0, got %d", hits)
	}
	if misses != 0 {
		t.Errorf("expected initial misses = 0, got %d", misses)
	}
}

func TestGetRestrictedCacheStats_BothCounters(t *testing.T) {
	state.SimulateRestart()

	const chatA = int64(-10099901)
	const chatB = int64(-10099902)

	baseHits, baseMisses := GetRestrictedCacheStats()

	MarkChatRestricted(chatA)
	defer MarkChatNotRestricted(chatA)

	if !IsChatRestricted(chatA) {
		t.Fatal("IsChatRestricted(chatA) should return true after MarkChatRestricted")
	}

	if IsChatRestricted(chatB) {
		t.Fatal("IsChatRestricted(chatB) should return false for unknown chat")
	}

	hits, misses := GetRestrictedCacheStats()
	if hits-baseHits < 1 {
		t.Errorf("expected at least 1 new hit, got delta=%d", hits-baseHits)
	}
	if misses-baseMisses < 1 {
		t.Errorf("expected at least 1 new miss, got delta=%d", misses-baseMisses)
	}
}

// ---------------------------------------------------------------------------
// MarkChatRestricted / IsChatRestricted
// ---------------------------------------------------------------------------

func TestMarkChatRestricted(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-1001618764357)
	defer MarkChatNotRestricted(chatID)

	MarkChatRestricted(chatID)

	if !IsChatRestricted(chatID) {
		t.Errorf("IsChatRestricted(%d) = false, want true after MarkChatRestricted", chatID)
	}
}

func TestMarkChatNotRestricted(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-10099910)

	MarkChatRestricted(chatID)
	MarkChatNotRestricted(chatID)

	if IsChatRestricted(chatID) {
		t.Errorf("IsChatRestricted(%d) = true after MarkChatNotRestricted, want false", chatID)
	}
}

func TestIsChatRestricted_Miss(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-10099920)

	if IsChatRestricted(chatID) {
		t.Errorf("IsChatRestricted(%d) = true for never-marked chat, want false", chatID)
	}
}

func TestIsChatRestricted_WithinProbeTTL(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-10099921)
	defer MarkChatNotRestricted(chatID)

	state.Set(
		context.Background(),
		restrictedChatKey(chatID),
		time.Now().Add(-(constants.RestrictedProbeInterval / 2)).Format(time.RFC3339),
		constants.RestrictedCacheTTL,
	)

	if !IsChatRestricted(chatID) {
		t.Fatal("IsChatRestricted should return true within probe TTL")
	}
}

func TestIsChatRestricted_AfterProbeTTL(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-10099922)
	defer MarkChatNotRestricted(chatID)

	state.Set(
		context.Background(),
		restrictedChatKey(chatID),
		time.Now().Add(-constants.RestrictedProbeInterval-time.Second).Format(time.RFC3339),
		constants.RestrictedCacheTTL,
	)

	if IsChatRestricted(chatID) {
		t.Fatal("IsChatRestricted should return false after probe TTL to allow retry")
	}
}

func TestIsChatRestricted_ProbeSingleFlight(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-10099923)
	defer MarkChatNotRestricted(chatID)

	state.Set(
		context.Background(),
		restrictedChatKey(chatID),
		time.Now().Add(-constants.RestrictedProbeInterval-time.Second).Format(time.RFC3339),
		constants.RestrictedCacheTTL,
	)

	if IsChatRestricted(chatID) {
		t.Fatal("first check after probe interval should allow probe attempt")
	}

	if !IsChatRestricted(chatID) {
		t.Fatal("second check should be blocked while probe lock is active")
	}
}

func TestMarkChatRestricted_Idempotent(t *testing.T) {
	state.SimulateRestart()

	const chatID = int64(-10099930)
	defer MarkChatNotRestricted(chatID)

	MarkChatRestricted(chatID)
	MarkChatRestricted(chatID)

	if !IsChatRestricted(chatID) {
		t.Errorf("IsChatRestricted(%d) = false after double MarkChatRestricted, want true", chatID)
	}
}
