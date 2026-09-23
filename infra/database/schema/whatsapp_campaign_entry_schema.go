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
	CloseSource             string         `gorm:"size:20"`
	CloseReason             string         `gorm:"size:40"`
	CloseOutcome            string         `gorm:"size:64;not null;default:'';index:idx_wce_close_outcome,priority:2"`
	ClosedAt                *time.Time     `gorm:"column:closed_at;index:idx_wce_close_outcome,priority:1"`
	CreatedAt               time.Time      `gorm:"autoCreateTime;index:idx_wce_campaign_status_created,priority:3;index:idx_wce_lead_created,priority:2"`
	UpdatedAt               time.Time      `gorm:"autoUpdateTime"`
	DeletedAt               gorm.DeletedAt `gorm:"index;index:idx_wce_campaign_del,priority:2"`
	LastMessageAt           *time.Time     `gorm:"column:last_message_at"`
	LastCustomerMessageAt   *time.Time     `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt      *time.Time     `gorm:"column:last_agent_message_at"`

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
