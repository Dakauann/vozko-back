package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ConversationMessage struct {
	ID          string `gorm:"primaryKey;type:text"`
	EntryID     string `gorm:"index:idx_message_entry;index:idx_cm_entry_composite,priority:1;index:idx_cm_unread,priority:1;index:idx_cm_type_entry_created,priority:2;index:idx_cm_entry_del_created,priority:1;type:uuid;not null"`
	EntryType   string `gorm:"index:idx_message_entry_type;index:idx_cm_entry_composite,priority:2;index:idx_cm_unread,priority:2;index:idx_cm_type_entry_created,priority:1;index:idx_cm_entry_del_created,priority:2;type:varchar(20);not null"`
	Channel     string `gorm:"type:varchar(20)"`
	MessageType string `gorm:"type:varchar(30);index:idx_cm_unread,priority:4"`
	// Direction is INBOUND or OUTBOUND, and '' on rows written before this
	// column existed. Not derivable from MessageType: that is the kind of
	// CONTENT, and a channel forced to encode direction in it has to discard
	// one of the two facts. See conversation.MessageHistoryDirection.
	//
	// Unindexed on purpose: nothing filters by it, every read that needs it is
	// already narrowed by (entry_id, entry_type), and an index on a two-value
	// column would only cost writes.
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
	// ExternalMessageID is the provider message id for channels added from
	// Instagram onward. WhatsApp still writes whatsapp_message_id. A partial
	// UNIQUE index on (entry_type, external_message_id) is created in migrate.go
	// so duplicate webhook deliveries are rejected by the database, not only by
	// the Redis dedup guard.
	ExternalMessageID *string        `gorm:"column:external_message_id;type:text;index:idx_cm_external_msgid"`
	ReplyToMessageID  *string        `gorm:"column:reply_to_message_id;type:text"`
	DeliveryStatus    string         `gorm:"type:varchar(20);default:''"`
	Metadata          []byte         `gorm:"type:jsonb"`
	CreatedAt         time.Time      `gorm:"autoCreateTime;index:idx_cm_entry_composite,priority:3;index:idx_cm_type_entry_created,priority:3;index:idx_cm_entry_del_created,priority:4"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime"`
	DeletedAt         gorm.DeletedAt `gorm:"index;index:idx_cm_entry_del_created,priority:3"`
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
