package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/divkix/Alita_Robot/alita/db/models"
)

// BackupFormatVersion is the current backup format version. Version 2 covers
// only the retained persistent domains and is intentionally incompatible with
// every historical export; no earlier version is accepted.
const BackupFormatVersion = "2"

// BackupFormat represents the structure of an exported backup file
type BackupFormat struct {
	Version    string                 `json:"version"`     // Backup format version (e.g., "2")
	ExportedAt time.Time              `json:"exported_at"` // Timestamp of export
	BotName    string                 `json:"bot_name"`    // Bot identifier (e.g., "AlitaRobot")
	ChatID     int64                  `json:"chat_id"`     // Source chat ID
	ChatName   string                 `json:"chat_name"`   // Source chat name
	ExportedBy int64                  `json:"exported_by"` // User ID who exported
	Domains    []string               `json:"domains"`     // List of exported domain names
	Data       map[string]interface{} `json:"data"`        // Domain-specific data
}

// NewBackupFormat creates a new backup format instance
func NewBackupFormat(chatID int64, chatName string, exportedBy int64, domains []string) *BackupFormat {
	return &BackupFormat{
		Version:    BackupFormatVersion,
		ExportedAt: time.Now().UTC(),
		BotName:    "AlitaRobot",
		ChatID:     chatID,
		ChatName:   chatName,
		ExportedBy: exportedBy,
		Domains:    domains,
		Data:       make(map[string]interface{}),
	}
}

// Validate checks if the backup format is valid
func (b *BackupFormat) Validate() error {
	if b == nil {
		return fmt.Errorf("backup cannot be nil")
	}
	if b.Version == "" {
		return fmt.Errorf("backup version is required")
	}
	if b.BotName == "" {
		return fmt.Errorf("bot name is required")
	}
	if b.ChatID == 0 {
		return fmt.Errorf("chat ID is required")
	}
	if len(b.Domains) == 0 {
		return fmt.Errorf("at least one domain must be specified")
	}
	if b.Data == nil {
		return fmt.Errorf("data field cannot be nil")
	}
	for _, domain := range b.Domains {
		if !IsValidDomain(domain) {
			return fmt.Errorf("unsupported domain: %s", domain)
		}
		if _, ok := b.Data[domain]; !ok {
			return fmt.Errorf("missing data for domain: %s", domain)
		}
	}
	// Payloads outside the declared domain set are rejected so a removed or
	// unknown domain can never reach a writer.
	for domain := range b.Data {
		if !IsValidDomain(domain) {
			return fmt.Errorf("unsupported domain: %s", domain)
		}
	}
	return nil
}

// IsCompatibleVersion reports whether the backup declares the current format
// version. Historical exports are rejected rather than partially migrated.
func (b *BackupFormat) IsCompatibleVersion() bool {
	return b != nil && b.Version == BackupFormatVersion
}

// ToJSON marshals the backup format to JSON bytes
func (b *BackupFormat) ToJSON() ([]byte, error) {
	return json.MarshalIndent(b, "", "  ")
}

// BackupFormatFromJSON unmarshals JSON bytes to BackupFormat
func BackupFormatFromJSON(data []byte) (*BackupFormat, error) {
	var backup BackupFormat
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&backup); err != nil {
		return nil, fmt.Errorf("failed to parse backup file: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("failed to parse backup file: trailing data")
	}
	return &backup, nil
}

// Retained domain names for export/import/reset.
const (
	DomainAntiflood   = "antiflood"
	DomainAntiraid    = "antiraid"
	DomainApprovals   = "approvals"
	DomainBlacklists  = "blacklists"
	DomainConnections = "connections"
	DomainFilters     = "filters"
	DomainWelcome     = "welcome"
	DomainNotes       = "notes"
	DomainReactions   = "reactions"
	DomainWarnings    = "warnings"
)

// allDomains is the canonical, ordered set of supported domains.
var allDomains = []string{
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

// AllDomains returns every domain name supported by the current backup format.
func AllDomains() []string {
	domains := make([]string, len(allDomains))
	copy(domains, allDomains)
	return domains
}

// IsValidDomain reports whether a domain name is supported by the current
// backup format. Removed domains and unknown names both return false.
func IsValidDomain(domain string) bool {
	for _, d := range allDomains {
		if d == domain {
			return true
		}
	}
	return false
}

// Per-domain backup payloads - using existing db models.

// AntifloodBackup represents antiflood settings backup data
type AntifloodBackup struct {
	Settings *models.AntifloodSettings `json:"settings,omitempty"`
}

// AntiraidBackup represents anti-raid settings backup data
type AntiraidBackup struct {
	Settings *models.AntiRaidSettings `json:"settings,omitempty"`
}

// ApprovalsBackup represents approved users backup data
type ApprovalsBackup struct {
	ApprovedUsers []models.ApprovedUsers `json:"approved_users,omitempty"`
}

// BlacklistsBackup represents blacklist entries backup data
type BlacklistsBackup struct {
	Entries []models.BlacklistSettings `json:"entries,omitempty"`
}

// ConnectionsBackup represents the users connected to the chat.
type ConnectionsBackup struct {
	Connections []models.ConnectionSettings `json:"connections,omitempty"`
}

// FiltersBackup represents filters backup data
type FiltersBackup struct {
	Filters []models.ChatFilters `json:"filters,omitempty"`
}

// WelcomeBackup represents welcome/greeting settings backup data
type WelcomeBackup struct {
	Settings *models.GreetingSettings `json:"settings,omitempty"`
}

// NotesBackup represents notes backup data
type NotesBackup struct {
	Settings *models.NotesSettings `json:"settings,omitempty"`
	Notes    []models.Notes        `json:"notes,omitempty"`
}

// ReactionsBackup represents keyword reaction mappings.
type ReactionsBackup struct {
	Reactions []models.Reactions `json:"reactions,omitempty"`
}

// WarningsBackup represents warning settings and events backup data
type WarningsBackup struct {
	Settings *models.WarnSettings `json:"settings,omitempty"`
	Warns    []models.Warns       `json:"warns,omitempty"`
}
