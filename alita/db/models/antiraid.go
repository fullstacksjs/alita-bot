package models

import "time"

// AntiRaidSettings stores per-chat anti-raid configuration together with the
// active-raid window. RaidStartedAt and RaidActiveUntil are runtime state
// rather than configuration: they are persisted so the expiry worker can
// recover in-flight raids after a restart, and they are excluded from backups.
type AntiRaidSettings struct {
	ID                    uint       `gorm:"primaryKey;autoIncrement" json:"-"`
	ChatID                int64      `gorm:"column:chat_id;uniqueIndex;not null" json:"chat_id,omitempty"`
	RaidTime              int        `gorm:"column:raid_time;default:21600" json:"raid_time,omitempty"`
	RaidActionTime        int        `gorm:"column:raid_action_time;default:3600" json:"raid_action_time,omitempty"`
	AutoAntiRaidThreshold int        `gorm:"column:auto_antiraid_threshold;default:0" json:"auto_antiraid_threshold,omitempty"`
	RaidStartedAt         *time.Time `gorm:"column:raid_started_at" json:"-"`
	RaidActiveUntil       *time.Time `gorm:"column:raid_active_until;index:idx_antiraid_settings_active_until" json:"-"`
	CreatedAt             time.Time  `gorm:"column:created_at" json:"created_at,omitempty"`
	UpdatedAt             time.Time  `gorm:"column:updated_at" json:"updated_at,omitempty"`
}

func (AntiRaidSettings) TableName() string {
	return "antiraid_settings"
}
