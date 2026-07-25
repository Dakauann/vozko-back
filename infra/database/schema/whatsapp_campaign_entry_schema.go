package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type WhatsAppCampaignEntry struct {
	ID                      string         `gorm:"primaryKey;type:uuid"`
	CampaignID              string         `gorm:"type:uuid;not null;index;index:idx_wce_campaign_del,priority:1;uniqueIndex:idx_wce_campaign_lead,priority:1;index:idx_wce_campaign_status_created,priority:1"`
	LeadID                  string         `gorm:"type:uuid;not null;index;uniqueIndex:idx_wce_campaign_lead,priority:2;index:idx_wce_lead_created,priority:1"`
	Status                  string         `gorm:"size:40;not null;default:'PENDING';index:idx_wce_campaign_status_created,priority:2"`
	MessageID               string         `gorm:"size:100;index:idx_wce_message_id"`
	ErrorCode               int            `gorm:"default:0;index:idx_wce_error_code"`
	ErrorMessage            string         `gorm:"size:500"`
	ReceivedBusinessPhoneID *string        `gorm:"type:uuid;index"`
	Variables               pq.StringArray `gorm:"type:text[]"`
	AutomationEnabled       *bool          `gorm:"default:null"`
	Metadata                LeadMetadata   `gorm:"type:jsonb;default:'{}'"`
	ConversationStatus      string         `gorm:"size:20;not null;default:'';index:idx_wce_conv_status"`
	// Close provenance when conversation_status = finished (null while open).
	CloseSource string         `gorm:"size:20"`
	CloseReason string         `gorm:"size:40"`
	ClosedAt    *time.Time     `gorm:"column:closed_at"`
	CreatedAt   time.Time      `gorm:"autoCreateTime;index:idx_wce_campaign_status_created,priority:3;index:idx_wce_lead_created,priority:2"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	DeletedAt   gorm.DeletedAt `gorm:"index;index:idx_wce_campaign_del,priority:2"`
	// LastMessageAt denormalizes the newest conversation_messages.created_at for this
	// entry. The inbox lists order and filter by it, which replaces a per-entry
	// JOIN LATERAL over conversation_messages that forced a full scan of every entry
	// in the workspace on each load. NULL means "no messages" — such entries are not
	// listed, matching the inner-join semantics the LATERAL had. Kept current by the
	// conversation message repository; see idx_wce_campaign_lastmsg in indexes.go.
	LastMessageAt *time.Time `gorm:"column:last_message_at"`
	// LastCustomerMessageAt / LastAgentMessageAt drive idle auto-close eligibility
	// without scanning conversation_messages. Agent clock includes operator, AI,
	// and template outbound. Maintained with last_message_at on message write.
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

	Lead Lead `gorm:"foreignKey:LeadID;references:ID"`
}

func (WhatsAppCampaignEntry) TableName() string {
	return "whatsapp_campaign_entries"
}

func (e *WhatsAppCampaignEntry) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
