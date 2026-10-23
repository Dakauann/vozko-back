package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type EmailInbox struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_email_inbox_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	Name         string  `gorm:"size:255;not null;default:''"`
	Address      string  `gorm:"size:254;not null;default:''"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	EnableAutoMemory     bool    `gorm:"not null;default:false"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_email_inbox_ws_del,priority:2"`
}

func (EmailInbox) TableName() string { return "email_inboxes" }

type EmailContact struct {
	ID          string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID string  `gorm:"type:uuid;not null;index"`
	Address     string  `gorm:"size:254;not null"`
	DisplayName string  `gorm:"size:255;not null;default:''"`
	LeadID      *string `gorm:"type:uuid;index"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (EmailContact) TableName() string { return "email_contacts" }

func (c *EmailContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type EmailConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_email_conv_contact"`
	SenderID    string `gorm:"type:uuid;not null;index:idx_email_conv_sender"`

	Subject              string         `gorm:"size:998;not null;default:''"`
	ThreadKey            string         `gorm:"size:998;not null;default:''"`
	LastInboundMessageID string         `gorm:"size:998;not null;default:''"`
	References           datatypes.JSON `gorm:"column:reference_chain;type:jsonb"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_email_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_email_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at;index"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`
	AwaitingReplySince    *time.Time `gorm:"column:awaiting_reply_since"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (EmailConversation) TableName() string { return "email_conversations" }

func (c *EmailConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

func EmailInboxModels() []any {
	return []any{
		&EmailInbox{},
		&EmailContact{},
		&EmailConversation{},
	}
}
