package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type UnofficialWhatsAppCampaign struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index;index:idx_uwc_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	InstanceID   string  `gorm:"type:uuid;not null;index:idx_uwc_instance"`
	CreatedByID  *string `gorm:"type:uuid;index"`

	Name string `gorm:"size:255;not null"`

	MessageKind string       `gorm:"size:16;not null;default:'text'"`
	Message     LeadMetadata `gorm:"type:jsonb;default:'{}'"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	EnableAutoMemory     bool    `gorm:"not null;default:false"`
	PreferAudio          bool    `gorm:"not null;default:false"`
	AiModel              string  `gorm:"size:120"`

	SendDelayMinMS int `gorm:"not null;default:3000"`
	SendDelayMaxMS int `gorm:"not null;default:12000"`
	DailyCap       int `gorm:"not null;default:0"`

	Status       string `gorm:"size:20;not null;default:'STOPPED';index"`
	StatusReason string `gorm:"size:255"`

	ResetCode string `gorm:"size:10"`
	ClearCode string `gorm:"size:10"`

	ScheduledStart time.Time `gorm:"type:timestamptz;index"`
	Archived       bool      `gorm:"default:false"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_uwc_ws_del,priority:2"`
}

func (UnofficialWhatsAppCampaign) TableName() string {
	return "unofficial_whatsapp_campaigns"
}

func (c *UnofficialWhatsAppCampaign) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type UnofficialWhatsAppCampaignEntry struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	CampaignID  string `gorm:"type:uuid;not null;index:idx_uwce_campaign_del,priority:1;uniqueIndex:idx_uwce_campaign_lead,priority:1;index:idx_uwce_campaign_status_created,priority:1"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`

	LeadID string `gorm:"type:uuid;not null;index;uniqueIndex:idx_uwce_campaign_lead,priority:2"`
	Number string `gorm:"size:32;not null;index"`
	Name   string `gorm:"size:200"`

	ContactID      *string `gorm:"type:uuid;index"`
	ConversationID *string `gorm:"type:uuid;index:idx_uwce_conversation"`

	JID       string     `gorm:"column:jid;size:64"`
	CheckedAt *time.Time `gorm:"column:checked_at"`

	Status       string `gorm:"size:40;not null;default:'PENDING';index:idx_uwce_campaign_status_created,priority:2"`
	VariantIndex int    `gorm:"not null;default:0"`

	ProviderMessageID string  `gorm:"size:128;index:idx_uwce_provider_msg"`
	MessageID         *string `gorm:"type:uuid;index"`
	ErrorCode         int     `gorm:"default:0;index"`
	ErrorMessage      string  `gorm:"size:500"`

	Variables pq.StringArray `gorm:"type:text[]"`
	Metadata  LeadMetadata   `gorm:"type:jsonb;default:'{}'"`

	SentAt    *time.Time
	CreatedAt time.Time      `gorm:"autoCreateTime;index:idx_uwce_campaign_status_created,priority:3"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_uwce_campaign_del,priority:2"`

	Lead Lead `gorm:"foreignKey:LeadID;references:ID"`
}

func (UnofficialWhatsAppCampaignEntry) TableName() string {
	return "unofficial_whatsapp_campaign_entries"
}

func (e *UnofficialWhatsAppCampaignEntry) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
