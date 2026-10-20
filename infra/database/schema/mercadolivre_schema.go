package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type MercadoLivreAccount struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_ml_acct_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`

	MLUserID string `gorm:"column:ml_user_id;size:20;not null;index:idx_ml_acct_ml_user"`
	SiteID   string `gorm:"size:8;not null"`
	Nickname string `gorm:"size:255"`

	AccessToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	RefreshToken     piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	AccessExpiresAt  *time.Time              `gorm:"type:timestamptz"`
	RefreshExpiresAt *time.Time              `gorm:"type:timestamptz"`
	GrantedScopes    string                  `gorm:"type:text;not null;default:''"`

	CanReadQuestions   bool `gorm:"not null;default:false"`
	CanAnswerQuestions bool `gorm:"not null;default:false"`
	CanReadMessages    bool `gorm:"not null;default:false"`
	CanSendMessages    bool `gorm:"not null;default:false"`

	Status       string `gorm:"size:24;not null;default:'DISCONNECTED'"`
	StatusReason string `gorm:"size:255"`

	LastReconciledAt  *time.Time `gorm:"type:timestamptz"`
	LastHealthCheckAt *time.Time `gorm:"type:timestamptz"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	AutomationDisclosure string  `gorm:"type:text"`

	PurgeAfter *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_ml_acct_ws_del,priority:2"`
}

func (MercadoLivreAccount) TableName() string { return "mercadolivre_accounts" }

func (a *MercadoLivreAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type MercadoLivreContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_ml_contact_account"`

	MLUserID           string     `gorm:"column:ml_user_id;size:20;not null"`
	Nickname           string     `gorm:"size:255"`
	ProfileRefreshedAt *time.Time `gorm:"type:timestamptz"`

	LeadID *string `gorm:"type:uuid;index"`

	QuestionsBlockedAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (MercadoLivreContact) TableName() string { return "mercadolivre_contacts" }

func (c *MercadoLivreContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type MercadoLivreConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_ml_conv_account"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_ml_conv_contact"`

	PackKey       string         `gorm:"size:20;not null"`
	OrderIDs      pq.StringArray `gorm:"column:order_ids;type:text[];not null;default:'{}'"`
	BuyerMLUserID string         `gorm:"column:buyer_ml_user_id;size:20;not null;default:''"`
	ShipmentID    string         `gorm:"size:20;not null;default:''"`

	MLStatus       string     `gorm:"column:ml_status;size:24;not null;default:''"`
	MLSubstatus    string     `gorm:"column:ml_substatus;size:96;not null;default:''"`
	MLStatusReadAt *time.Time `gorm:"column:ml_status_read_at;type:timestamptz"`

	AgentMediated        *bool `gorm:"default:null"`
	OutboundBudget       int   `gorm:"not null;default:0"`
	OutboundSinceInbound int   `gorm:"not null;default:0"`
	WindowOpen           bool  `gorm:"not null;default:false"`

	PackSummary            datatypes.JSON `gorm:"type:jsonb"`
	PackSummaryRefreshedAt *time.Time     `gorm:"type:timestamptz"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index:idx_ml_conv_status"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_ml_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_ml_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`
	AwaitingReplySince    *time.Time `gorm:"column:awaiting_reply_since"`

	Metadata  LeadMetadata   `gorm:"type:jsonb;default:'{}'"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (MercadoLivreConversation) TableName() string { return "mercadolivre_conversations" }

func (c *MercadoLivreConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
