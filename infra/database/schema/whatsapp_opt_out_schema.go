package schema

import "time"

type WhatsAppOptOutEvent struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	PhoneNumberID string    `gorm:"type:varchar(64);not null;uniqueIndex:ux_wa_opt_out_event,priority:1;index:idx_wa_opt_out_phone_time,priority:1"`
	RecipientHash string    `gorm:"type:char(64);not null;uniqueIndex:ux_wa_opt_out_event,priority:2"`
	Preference    string    `gorm:"type:varchar(16);not null;uniqueIndex:ux_wa_opt_out_event,priority:3"`
	Category      string    `gorm:"type:varchar(64);not null;default:''"`
	OccurredAt    time.Time `gorm:"not null;uniqueIndex:ux_wa_opt_out_event,priority:4;index:idx_wa_opt_out_phone_time,priority:2"`
	RecordedAt    time.Time `gorm:"autoCreateTime;not null"`
}

func (WhatsAppOptOutEvent) TableName() string { return "whatsapp_opt_out_events" }
