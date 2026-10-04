package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type WebchatWidget struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_wc_widget_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	Name         string  `gorm:"size:255;not null"`
	PublicKey    string  `gorm:"size:64;not null"`
	Status       string  `gorm:"size:16;not null;default:'active'"`

	AllowedOrigins datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`

	AccentColor    string `gorm:"size:7;not null;default:'#1F6FEB'"`
	Position       string `gorm:"size:8;not null;default:'right'"`
	LauncherLabel  string `gorm:"size:255"`
	WelcomeTitle   string `gorm:"size:255"`
	WelcomeMessage string `gorm:"size:2048"`
	TeamName       string `gorm:"size:255"`
	AssistantName  string `gorm:"size:255"`

	IntakeName         string `gorm:"size:16;not null;default:'optional'"`
	IntakeEmail        string `gorm:"size:16;not null;default:'optional'"`
	IntakePhone        string `gorm:"size:16;not null;default:'hidden'"`
	PrivacyPolicyURL   string `gorm:"size:2048"`
	DefaultCountryCode string `gorm:"size:3;not null;default:'55'"`

	AllowHumanRequest bool `gorm:"not null;default:false"`
	AllowAttachments  bool `gorm:"not null;default:false"`

	IdentityMode   string                  `gorm:"size:16;not null;default:'off'"`
	IdentitySecret piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

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
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_wc_widget_ws_del,priority:2"`
}

func (WebchatWidget) TableName() string { return "webchat_widgets" }

func (w *WebchatWidget) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

type WebchatVisitor struct {
	ID          string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID string  `gorm:"type:uuid;not null;index"`
	WidgetID    string  `gorm:"type:uuid;not null;index"`
	ExternalID  *string `gorm:"size:128"`

	Name   string  `gorm:"size:255"`
	Email  string  `gorm:"size:254;index"`
	Phone  string  `gorm:"size:32;index"`
	LeadID *string `gorm:"type:uuid;index"`

	IdentityVerified  bool       `gorm:"not null;default:false"`
	IntakeCompletedAt *time.Time `gorm:"type:timestamptz"`
	ConsentedAt       *time.Time `gorm:"type:timestamptz"`

	Locale     string     `gorm:"size:16"`
	UserAgent  string     `gorm:"size:1024"`
	IPHash     string     `gorm:"size:64;index"`
	PageOrigin string     `gorm:"size:255"`
	LastSeenAt *time.Time `gorm:"type:timestamptz"`

	Blocked   bool       `gorm:"not null;default:false"`
	BlockedAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (WebchatVisitor) TableName() string { return "webchat_visitors" }

func (v *WebchatVisitor) BeforeCreate(tx *gorm.DB) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	return nil
}

type WebchatConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	WidgetID    string `gorm:"type:uuid;not null;index:idx_wc_conv_widget"`
	VisitorID   string `gorm:"type:uuid;not null;index:idx_wc_conv_visitor"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_wc_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_wc_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	PendingOptions datatypes.JSON `gorm:"type:jsonb"`
	PageURL        string         `gorm:"size:2048"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at;index"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (WebchatConversation) TableName() string { return "webchat_conversations" }

func (c *WebchatConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
