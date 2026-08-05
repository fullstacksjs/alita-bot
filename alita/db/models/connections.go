package models

import "time"

// ConnectionSettings represents connection settings
type ConnectionSettings struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"-"`
	UserId    int64     `gorm:"column:user_id;not null;uniqueIndex:uk_connection_user_id" json:"user_id,omitempty"`
	ChatId    int64     `gorm:"column:chat_id;not null" json:"chat_id,omitempty"`
	Connected bool      `gorm:"column:connected;default:false" json:"connected,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at,omitempty"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at,omitempty"`
}

func (ConnectionSettings) TableName() string {
	return "connection"
}
