package backup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// BackupFormat
// ---------------------------------------------------------------------------

func TestNewBackupFormat(t *testing.T) {

	chatID := int64(123456)
	chatName := "Test Chat"
	exportedBy := int64(789)
	domains := []string{DomainNotes, DomainFilters}

	bf := NewBackupFormat(chatID, chatName, exportedBy, domains)

	if bf.Version != BackupFormatVersion {
		t.Fatalf("Version = %q, want %q", bf.Version, BackupFormatVersion)
	}
	if bf.BotName != "AlitaRobot" {
		t.Fatalf("BotName = %q, want %q", bf.BotName, "AlitaRobot")
	}
	if bf.ChatID != chatID {
		t.Fatalf("ChatID = %d, want %d", bf.ChatID, chatID)
	}
	if bf.ChatName != chatName {
		t.Fatalf("ChatName = %q, want %q", bf.ChatName, chatName)
	}
	if bf.ExportedBy != exportedBy {
		t.Fatalf("ExportedBy = %d, want %d", bf.ExportedBy, exportedBy)
	}
	if len(bf.Domains) != len(domains) {
		t.Fatalf("Domains len = %d, want %d", len(bf.Domains), len(domains))
	}
	if bf.Data == nil {
		t.Fatal("Data should be initialized to non-nil map")
	}
	if bf.ExportedAt.IsZero() {
		t.Fatal("ExportedAt should be set to current time")
	}
}

func TestBackupFormat_Validate(t *testing.T) {

	now := time.Now().UTC()

	tests := []struct {
		name    string
		bf      *BackupFormat
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid backup format returns no error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{DomainNotes},
				Data:       map[string]interface{}{DomainNotes: map[string]interface{}{}},
				ExportedAt: now,
			},
			wantErr: false,
		},
		{
			name: "missing domain payload returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{DomainNotes},
				Data:       make(map[string]interface{}),
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "missing data for domain: notes",
		},
		{
			name: "removed domain in domain list returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{"rules"},
				Data:       map[string]interface{}{"rules": map[string]interface{}{}},
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "unsupported domain: rules",
		},
		{
			name: "removed domain payload returns error",
			bf: &BackupFormat{
				Version: BackupFormatVersion,
				BotName: "AlitaRobot",
				ChatID:  123,
				Domains: []string{DomainNotes},
				Data: map[string]interface{}{
					DomainNotes: map[string]interface{}{},
					"admin":     map[string]interface{}{},
				},
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "unsupported domain: admin",
		},
		{
			name: "unknown domain returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{"nonexistent"},
				Data:       map[string]interface{}{"nonexistent": map[string]interface{}{}},
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "unsupported domain: nonexistent",
		},
		{
			name: "empty version returns error",
			bf: &BackupFormat{
				Version:    "",
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{DomainNotes},
				Data:       make(map[string]interface{}),
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "backup version is required",
		},
		{
			name: "empty bot name returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "",
				ChatID:     123,
				Domains:    []string{DomainNotes},
				Data:       make(map[string]interface{}),
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "bot name is required",
		},
		{
			name: "zero chat ID returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     0,
				Domains:    []string{DomainNotes},
				Data:       make(map[string]interface{}),
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "chat ID is required",
		},
		{
			name: "empty domains returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{},
				Data:       make(map[string]interface{}),
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "at least one domain must be specified",
		},
		{
			name: "nil data returns error",
			bf: &BackupFormat{
				Version:    BackupFormatVersion,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{DomainNotes},
				Data:       nil,
				ExportedAt: now,
			},
			wantErr: true,
			errMsg:  "data field cannot be nil",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			err := tc.bf.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tc.errMsg {
					t.Fatalf("error = %q, want %q", err.Error(), tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBackupFormat_IsCompatibleVersion(t *testing.T) {

	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{
			name:    "exact version match returns true",
			version: BackupFormatVersion,
			want:    true,
		},
		{
			name:    "historical 1.0 export is rejected",
			version: "1.0",
			want:    false,
		},
		{
			name:    "historical 1.1 export is rejected",
			version: "1.1",
			want:    false,
		},
		{
			name:    "empty version returns false",
			version: "",
			want:    false,
		},
		{
			name:    "future version returns false",
			version: "3",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			bf := &BackupFormat{
				Version:    tc.version,
				BotName:    "AlitaRobot",
				ChatID:     123,
				Domains:    []string{DomainNotes},
				Data:       make(map[string]interface{}),
				ExportedAt: time.Now().UTC(),
			}
			got := bf.IsCompatibleVersion()
			if got != tc.want {
				t.Fatalf("IsCompatibleVersion() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackupFormat_ToJSON(t *testing.T) {

	bf := NewBackupFormat(123, "Test", 456, []string{DomainNotes})
	bf.Data[DomainNotes] = map[string]interface{}{"notes": []interface{}{}}

	data, err := bf.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error: %v", err)
	}

	// Verify valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("ToJSON() produced invalid JSON: %v", err)
	}

	// Verify key fields present
	if parsed["version"] != BackupFormatVersion {
		t.Fatalf("JSON version = %v, want %v", parsed["version"], BackupFormatVersion)
	}
	if parsed["bot_name"] != "AlitaRobot" {
		t.Fatalf("JSON bot_name = %v, want %v", parsed["bot_name"], "AlitaRobot")
	}
	if parsed["chat_id"] != float64(123) {
		t.Fatalf("JSON chat_id = %v, want 123", parsed["chat_id"])
	}
	// The domain list is published under "domains"; "modules" belonged to the
	// historical format and must not reappear.
	if _, ok := parsed["domains"]; !ok {
		t.Fatalf("JSON missing domains field: %v", parsed)
	}
	if _, ok := parsed["modules"]; ok {
		t.Fatalf("JSON still exposes legacy modules field: %v", parsed)
	}

	// Verify indent formatting (contains newlines for readability)
	if !strings.Contains(string(data), "\n") {
		t.Fatalf("JSON missing indentation/newlines: got %q", string(data))
	}
}

func TestBackupFormatFromJSON(t *testing.T) {

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantVer string
		wantID  int64
	}{
		{
			name:    "valid JSON parses correctly",
			input:   `{"version":"2","bot_name":"AlitaRobot","chat_id":123,"chat_name":"Test","exported_by":456,"domains":["notes"],"data":{"notes":{}},"exported_at":"2024-01-01T00:00:00Z"}`,
			wantErr: false,
			wantVer: "2",
			wantID:  123,
		},
		{
			name:    "invalid JSON returns error",
			input:   `not json at all`,
			wantErr: true,
		},
		{
			name:    "empty JSON returns error",
			input:   ``,
			wantErr: true,
		},
		{
			name:    "trailing JSON returns error",
			input:   `{"version":"2"} {}`,
			wantErr: true,
		},
		{
			name:    "historical version still parses so it can be reported",
			input:   `{"version":"1.1","bot_name":"TestBot","chat_id":789,"exported_by":0,"modules":["filters"],"data":{},"exported_at":"2024-06-01T12:00:00Z"}`,
			wantErr: false,
			wantVer: "1.1",
			wantID:  789,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			bf, err := BackupFormatFromJSON([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bf.Version != tc.wantVer {
				t.Fatalf("Version = %q, want %q", bf.Version, tc.wantVer)
			}
			if bf.ChatID != tc.wantID {
				t.Fatalf("ChatID = %d, want %d", bf.ChatID, tc.wantID)
			}
		})
	}

	const largeID = "9007199254740993"
	bf, err := BackupFormatFromJSON([]byte(`{"data":{"warnings":[{"user_id":` + largeID + `}]}}`))
	if err != nil {
		t.Fatalf("large nested integer parse: %v", err)
	}
	got := bf.Data[DomainWarnings].([]any)[0].(map[string]any)["user_id"]
	if got != json.Number(largeID) {
		t.Fatalf("nested user_id = %v (%T), want exact json.Number(%s)", got, got, largeID)
	}
}

// ---------------------------------------------------------------------------
// Domain helpers
// ---------------------------------------------------------------------------

func TestAllDomains(t *testing.T) {

	domains := AllDomains()

	expected := []string{
		DomainAntiflood,
		DomainAntiraid,
		DomainApprovals,
		DomainBlacklists,
		DomainConnections,
		DomainFilters,
		DomainWelcome,
		DomainNotes,
		DomainReactions,
		DomainWarnings,
	}

	if len(domains) != len(expected) {
		t.Fatalf("AllDomains() = %v, want %v", domains, expected)
	}
	for i, want := range expected {
		if domains[i] != want {
			t.Fatalf("AllDomains()[%d] = %q, want %q", i, domains[i], want)
		}
	}

	// The returned slice must not alias the package-level set.
	domains[0] = "mutated"
	if AllDomains()[0] != DomainAntiflood {
		t.Fatal("AllDomains() returned an aliased slice")
	}
}

func TestIsValidDomain(t *testing.T) {

	tests := []struct {
		name   string
		domain string
		want   bool
	}{
		{name: "retained domain filters", domain: DomainFilters, want: true},
		{name: "retained domain warnings", domain: DomainWarnings, want: true},
		{name: "retained domain welcome", domain: DomainWelcome, want: true},
		{name: "retained domain connections", domain: DomainConnections, want: true},
		{name: "removed domain admin", domain: "admin", want: false},
		{name: "removed domain disabling", domain: "disabling", want: false},
		{name: "removed domain pins", domain: "pins", want: false},
		{name: "removed domain rules", domain: "rules", want: false},
		{name: "renamed domain greetings", domain: "greetings", want: false},
		{name: "renamed domain warns", domain: "warns", want: false},
		{name: "unknown domain", domain: "nonexistent", want: false},
		{name: "empty string", domain: "", want: false},
		{name: "case-sensitive mismatch", domain: "Notes", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsValidDomain(tc.domain)
			if got != tc.want {
				t.Fatalf("IsValidDomain(%q) = %v, want %v", tc.domain, got, tc.want)
			}
		})
	}
}
