package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WhatsAppQualityChange struct {
	ID         string    `gorm:"primaryKey;type:uuid"`
	Subject    string    `gorm:"type:varchar(16);not null;uniqueIndex:ux_wa_quality_change,priority:1;index:idx_wa_quality_change_subject,priority:1"`
	ExternalID string    `gorm:"type:varchar(64);not null;uniqueIndex:ux_wa_quality_change,priority:2;index:idx_wa_quality_change_subject,priority:2"`
	WABAID     string    `gorm:"type:varchar(64);not null;default:''"`
	Name       string    `gorm:"type:varchar(255);not null;default:''"`
	Signal     string    `gorm:"type:varchar(32);not null;uniqueIndex:ux_wa_quality_change,priority:3"`
	Event      string    `gorm:"type:varchar(64);not null;default:''"`
	Previous   string    `gorm:"type:varchar(64);not null;default:''"`
	Current    string    `gorm:"type:varchar(64);not null;uniqueIndex:ux_wa_quality_change,priority:4"`
	Source     string    `gorm:"type:varchar(16);not null"`
	OccurredAt time.Time `gorm:"not null;uniqueIndex:ux_wa_quality_change,priority:5;index:idx_wa_quality_change_subject,priority:3;index"`
	RecordedAt time.Time `gorm:"autoCreateTime;not null"`
}

func (WhatsAppQualityChange) TableName() string { return "whatsapp_quality_changes" }

func (c *WhatsAppQualityChange) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
