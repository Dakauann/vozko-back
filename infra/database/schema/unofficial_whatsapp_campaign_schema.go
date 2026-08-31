package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// UnofficialWhatsAppCampaign is one bulk send over one linked-device number.
//
// The sibling of WhatsAppCampaign, and the column set says exactly how the two
// differ: there is no template_id and no billing anywhere, and there are pacing
// and cap columns the Cloud API has no use for.
type UnofficialWhatsAppCampaign struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index;index:idx_uwc_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	InstanceID   string  `gorm:"type:uuid;not null;index:idx_uwc_instance"`
	CreatedByID  *string `gorm:"type:uuid;index"`

	Name string `gorm:"size:255;not null"`

	// MessageKind is a column as well as a key inside Message so the list query
	// can render "Texto"/"Imagem" per row without deserializing every payload.
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

	// Pacing is copied from the instance at creation, never referenced, so
	// widening a number's range later cannot silently speed up a blast already
	// running against a number under scrutiny.
	SendDelayMinMS int `gorm:"not null;default:3000"`
	SendDelayMaxMS int `gorm:"not null;default:12000"`
	DailyCap       int `gorm:"not null;default:0"`

	Status string `gorm:"size:20;not null;default:'STOPPED';index"`
	// StatusReason explains a pause the SYSTEM applied. Without it a campaign
	// stopped for a WhatsApp restriction is indistinguishable from one somebody
	// paused by hand.
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

// UnofficialWhatsAppCampaignEntry is one target of a campaign.
//
// It POINTS AT a conversation rather than being one, which is the decisive
// difference from WhatsAppCampaignEntry. The conversation columns that live on
// the Cloud API entry — conversation_status, close_*, automation_enabled — are
// deliberately absent: unofficial_whatsapp_conversations already owns them, and
// a second copy would be a second answer to "is this chat finished".
type UnofficialWhatsAppCampaignEntry struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	CampaignID  string `gorm:"type:uuid;not null;index:idx_uwce_campaign_del,priority:1;uniqueIndex:idx_uwce_campaign_lead,priority:1;index:idx_uwce_campaign_status_created,priority:1"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`

	// The uniqueness on (campaign_id, lead_id) is the control that stops one
	// campaign blasting the same person twice — the most common cause of a ban
	// complaint there is.
	LeadID string `gorm:"type:uuid;not null;index;uniqueIndex:idx_uwce_campaign_lead,priority:2"`
	Number string `gorm:"size:32;not null;index"`
	Name   string `gorm:"size:200"`

	ContactID      *string `gorm:"type:uuid;index"`
	ConversationID *string `gorm:"type:uuid;index:idx_uwce_conversation"`

	// The column is named EXPLICITLY, as every other JID column in this channel
	// is. GORM's naming strategy turns the field `JID` into `j_id`, which then
	// disagrees with every raw column reference in the repository — the update
	// fails at runtime with 42703, on the send path, long after the table was
	// created. unofficial_whatsapp_broadcast_targets carries both `j_id` and
	// `jid` from exactly this mistake made once already.
	JID       string     `gorm:"column:jid;size:64"`
	CheckedAt *time.Time `gorm:"column:checked_at"`

	Status string `gorm:"size:40;not null;default:'PENDING';index:idx_uwce_campaign_status_created,priority:2"`
	// VariantIndex records which body this recipient received, so a workspace
	// running rotations can tell which one performed.
	VariantIndex int `gorm:"not null;default:0"`

	// Unique where present, so a redelivered echo webhook reconciles onto the
	// row we wrote instead of matching several.
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
