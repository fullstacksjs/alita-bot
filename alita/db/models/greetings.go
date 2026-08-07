package models

import "time"

// WelcomeSettings represents welcome message settings
type WelcomeSettings struct {
	CleanWelcome  bool        `gorm:"column:clean_old;default:false" json:"clean_old" default:"false"`
	LastMsgId     int64       `gorm:"column:last_msg_id" json:"last_msg_id,omitempty"`
	ShouldWelcome bool        `gorm:"column:enabled;default:true" json:"welcome_enabled" default:"true"`
	WelcomeText   string      `gorm:"column:text" json:"welcome_text,omitempty"`
	FileID        string      `gorm:"column:file_id" json:"file_id,omitempty"`
	WelcomeType   int         `gorm:"column:type;default:1" json:"welcome_type,omitempty"`
	Button        ButtonArray `gorm:"column:btns;type:text" json:"btns,omitempty"`
}

// GreetingSettings represents greeting settings for a chat
type GreetingSettings struct {
	ID                 uint             `gorm:"primaryKey;autoIncrement" json:"-"`
	ChatID             int64            `gorm:"column:chat_id;uniqueIndex;not null" json:"_id,omitempty"`
	ShouldCleanService bool             `gorm:"column:clean_service_settings;default:false" json:"clean_service_settings" default:"false"`
	WelcomeSettings    *WelcomeSettings `gorm:"embedded;embeddedPrefix:welcome_" json:"welcome_settings" default:"false"`
	CreatedAt          time.Time        `gorm:"column:created_at" json:"created_at,omitempty"`
	UpdatedAt          time.Time        `gorm:"column:updated_at" json:"updated_at,omitempty"`
}

func (GreetingSettings) TableName() string {
	return "greetings"
}
