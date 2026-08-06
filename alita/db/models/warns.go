package models

import "time"

// WarnSettings represents warning settings for a chat
type WarnSettings struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"-"`
	ChatId    int64     `gorm:"column:chat_id;uniqueIndex;not null" json:"_id,omitempty"`
	WarnLimit int       `gorm:"column:warn_limit;default:3;check:chk_warn_limit,warn_limit > 0" json:"warn_limit" default:"3"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at,omitempty"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at,omitempty"`
}

func (WarnSettings) TableName() string {
	return "warns_settings"
}

// WarnEvent represents an individual warning event for a user in a chat
type WarnEvent struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"-"`
	UserId    int64     `gorm:"column:user_id;not null;index:idx_warn_events_user_chat" json:"user_id,omitempty"`
	ChatId    int64     `gorm:"column:chat_id;not null;index:idx_warn_events_user_chat" json:"chat_id,omitempty"`
	Reason    string    `gorm:"column:reason;default:''" json:"reason,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at,omitempty"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at,omitempty"`
}

func (WarnEvent) TableName() string {
	return "warn_events"
}

// Warns is an alias for WarnEvent
type Warns = WarnEvent
