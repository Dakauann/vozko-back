package schema

import "time"

type ConversationAdOrigin struct {
	EntryID      string    `gorm:"type:uuid;primaryKey"`
	EntryType    string    `gorm:"type:varchar(32);primaryKey"`
	AdID         string    `gorm:"type:varchar(64);not null;default:''"`
	Platform     string    `gorm:"type:varchar(16);not null;default:''"`
	Title        string    `gorm:"type:text;not null;default:''"`
	SourceURL    string    `gorm:"type:text;not null;default:''"`
	ClickID      string    `gorm:"type:varchar(255);not null;default:''"`
	SourceType   string    `gorm:"type:varchar(32);not null;default:''"`
	ImageMediaID *string   `gorm:"type:uuid"`
	ArrivedAt    time.Time `gorm:"not null"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}

func (ConversationAdOrigin) TableName() string {
	return "conversation_ad_origins"
}
