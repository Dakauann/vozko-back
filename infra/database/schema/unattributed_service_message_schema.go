package schema

import "time"

type UnattributedServiceMessage struct {
	WhatsAppMessageID string    `gorm:"column:whatsapp_message_id;primaryKey;type:varchar(128)"`
	PhoneNumberID     string    `gorm:"column:phone_number_id;type:varchar(64);not null;default:'';index"`
	Status            string    `gorm:"column:status;type:varchar(20);not null;default:''"`
	Category          string    `gorm:"column:category;type:varchar(32);not null;default:''"`
	PricingType       string    `gorm:"column:pricing_type;type:varchar(32);not null;default:''"`
	FirstSeenAt       time.Time `gorm:"column:first_seen_at;not null;index"`
	LastSeenAt        time.Time `gorm:"column:last_seen_at;not null"`
}

func (UnattributedServiceMessage) TableName() string { return "whatsapp_unattributed_service_messages" }
