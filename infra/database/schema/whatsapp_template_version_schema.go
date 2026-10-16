package schema

import "time"

type WhatsAppTemplateVersion struct {
	ID              int64                      `gorm:"primaryKey;autoIncrement"`
	ExternalID      string                     `gorm:"type:varchar(100);not null;uniqueIndex:ux_wa_template_version,priority:1"`
	WABAID          string                     `gorm:"type:varchar(36);not null;default:'';uniqueIndex:ux_wa_template_version,priority:2"`
	ContentHash     string                     `gorm:"type:char(32);not null;uniqueIndex:ux_wa_template_version,priority:3"`
	Name            string                     `gorm:"type:varchar(255);not null"`
	Language        string                     `gorm:"type:varchar(10);not null"`
	Category        string                     `gorm:"type:varchar(20);not null"`
	ParameterFormat string                     `gorm:"type:varchar(20);not null"`
	Components      WhatsAppTemplateComponents `gorm:"type:jsonb"`
	Status          string                     `gorm:"type:varchar(20);not null"`
	CapturedAt      time.Time                  `gorm:"autoCreateTime;not null;index"`
}

func (WhatsAppTemplateVersion) TableName() string { return "whatsapp_template_versions" }
