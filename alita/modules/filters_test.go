package modules

import (
	"testing"

	"github.com/divkix/Alita_Robot/alita/db"
)

func TestFilterOverwriteCacheKeysAndToken(t *testing.T) {
	if got := filterOverwriteCacheKey("abc123"); got != "alita:filter_overwrite:abc123" {
		t.Fatalf("filterOverwriteCacheKey() = %q", got)
	}

	token, err := newOverwriteToken()
	if err != nil {
		t.Fatalf("newOverwriteToken() error = %v", err)
	}
	if len(token) != 16 {
		t.Fatalf("newOverwriteToken() len = %d, want 16 hex chars", len(token))
	}
	for _, ch := range token {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			t.Fatalf("newOverwriteToken() contains non-hex character %q in %q", ch, token)
		}
	}
}

func TestFilterOverwriteCacheNoCacheFallbacks(t *testing.T) {

	data := overwriteFilter{overwriteBase: overwriteBase{
		ChatID:   -100123,
		ItemName: "hello",
		Text:     "world",
		DataType: 1,
	}}

	if err := setFilterOverwriteCache("token", data); err != nil {
		t.Fatalf("setFilterOverwriteCache() error = %v, want nil", err)
	}
	got, err := getFilterOverwriteCache("token")
	if err != nil {
		t.Fatalf("getFilterOverwriteCache() error = %v, want nil", err)
	}
	if got.ChatID != data.ChatID || got.Text != data.Text {
		t.Fatalf("getFilterOverwriteCache() = %+v, want %+v", got, data)
	}

	deleteFilterOverwriteCache("token")
}

func TestFilterOverwriteCacheRoundTripsCurrentData(t *testing.T) {
	current := overwriteFilter{overwriteBase: overwriteBase{
		ChatID:   -100123,
		ItemName: "hello",
		Text:     "current",
		DataType: db.TEXT,
	}}
	if err := setFilterOverwriteCache("token-current", current); err != nil {
		t.Fatalf("setFilterOverwriteCache() error = %v", err)
	}
	got, err := getFilterOverwriteCache("token-current")
	if err != nil {
		t.Fatalf("getFilterOverwriteCache() error = %v", err)
	}
	if got.ChatID != current.ChatID || got.ItemName != current.ItemName || got.Text != current.Text {
		t.Fatalf("getFilterOverwriteCache() = %+v, want %+v", got, current)
	}
	deleteFilterOverwriteCache("token-current")
	if _, err := getFilterOverwriteCache("token-current"); err == nil {
		t.Fatal("getFilterOverwriteCache(deleted) error = nil, want cache miss")
	}
}
