package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ConversationMessage struct {
	ID                string     `gorm:"primaryKey;type:text"`
	EntryID           string     `gorm:"index:idx_message_entry;index:idx_cm_entry_composite,priority:1;index:idx_cm_unread,priority:1;index:idx_cm_type_entry_created,priority:2;index:idx_cm_entry_del_created,priority:1;type:uuid;not null"`
	EntryType         string     `gorm:"index:idx_message_entry_type;index:idx_cm_entry_composite,priority:2;index:idx_cm_unread,priority:2;index:idx_cm_type_entry_created,priority:1;index:idx_cm_entry_del_created,priority:2;type:varchar(20);not null"`
	Channel           string     `gorm:"type:varchar(20)"`
	MessageType       string     `gorm:"type:varchar(30);index:idx_cm_unread,priority:4"`
	Direction         string     `gorm:"type:varchar(10);default:''"`
	FromParticipant   string     `gorm:"size:120"`
	ToParticipant     string     `gorm:"size:120"`
	Text              string     `gorm:"type:text"`
	Image             []byte     `gorm:"type:bytea"`
	Video             []byte     `gorm:"type:bytea"`
	MediaID           *string    `gorm:"type:uuid;index"`
	MediaType         string     `gorm:"type:varchar(30)"`
	Read              bool       `gorm:"default:false;index:idx_cm_unread,priority:3"`
	ReadAt            *time.Time `gorm:"type:timestamptz"`
	ReadBy            *string    `gorm:"type:uuid"`
	WhatsAppMessageID *string    `gorm:"column:whatsapp_message_id;type:text;index:idx_message_wamid"`
	ExternalMessageID *string    `gorm:"column:external_message_id;type:text;index:idx_cm_external_msgid"`
	ReplyToMessageID  *string    `gorm:"column:reply_to_message_id;type:text"`
	DeliveryStatus    string     `gorm:"type:varchar(20);default:''"`
	SentVia           string     `gorm:"column:sent_via;type:varchar(20);not null;default:''"`
	SenderKind        string     `gorm:"column:sender_kind;type:varchar(16);not null;default:'unknown'"`
	SenderID          string     `gorm:"column:sender_id;type:varchar(120);not null;default:''"`

	MetaPricingCategory    string `gorm:"column:meta_pricing_category;type:varchar(20);default:''"`
	MetaPricingBillable    *bool  `gorm:"column:meta_pricing_billable"`
	MetaPricingModel       string `gorm:"column:meta_pricing_model;type:varchar(32);default:''"`
	MetaConversationOrigin string `gorm:"column:meta_conversation_origin;type:varchar(32);default:''"`

	Metadata  []byte         `gorm:"type:jsonb"`
	CreatedAt time.Time      `gorm:"autoCreateTime;index:idx_cm_entry_composite,priority:3;index:idx_cm_type_entry_created,priority:3;index:idx_cm_entry_del_created,priority:4"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_cm_entry_del_created,priority:3"`
}

func (ConversationMessage) TableName() string {
	return "conversation_messages"
}

func (m *ConversationMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
