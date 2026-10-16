package schema

import "time"

type WhatsAppMessageStatusEvent struct {
	ID                 int64      `gorm:"primaryKey;autoIncrement"`
	MessageID          string     `gorm:"type:varchar(191);not null;uniqueIndex:ux_wa_msg_status_event,priority:1"`
	Status             string     `gorm:"type:varchar(16);not null;uniqueIndex:ux_wa_msg_status_event,priority:2"`
	PhoneNumberID      string     `gorm:"type:varchar(64);not null"`
	ErrorCode          int        `gorm:"not null;default:0"`
	Pricing            string     `gorm:"type:varchar(32);not null;default:''"`
	OccurredAt         time.Time  `gorm:"not null;index:idx_wa_msg_status_occurred"`
	RecordedAt         time.Time  `gorm:"autoCreateTime;not null;index:idx_wa_msg_status_pending,where:attributed_at IS NULL"`
	TemplateExternalID string     `gorm:"type:varchar(100);not null;default:''"`
	WABAID             string     `gorm:"type:varchar(64);not null;default:''"`
	AttributedAt       *time.Time `gorm:"index:idx_wa_msg_status_unrolled,priority:1,where:rolled_up_at IS NULL AND attributed_at IS NOT NULL"`
	RolledUpAt         *time.Time
}

func (WhatsAppMessageStatusEvent) TableName() string { return "whatsapp_message_status_events" }

type WhatsAppTemplateHourlyStat struct {
	TemplateExternalID string    `gorm:"primaryKey;type:varchar(100)"`
	PhoneNumberID      string    `gorm:"primaryKey;type:varchar(64)"`
	Hour               time.Time `gorm:"primaryKey;index:idx_wa_tpl_hourly_hour"`
	Status             string    `gorm:"primaryKey;type:varchar(16)"`
	ErrorCode          int       `gorm:"primaryKey"`
	WABAID             string    `gorm:"type:varchar(64);not null;default:''"`
	Messages           int64     `gorm:"not null"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime;not null"`
}

func (WhatsAppTemplateHourlyStat) TableName() string { return "whatsapp_template_hourly_stats" }
