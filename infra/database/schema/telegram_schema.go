package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type TelegramAccount struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_tg_acct_workspace;index:idx_tg_acct_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`

	Mode string `gorm:"size:16;not null;default:'BOT';index"`

	BotUserID            int64  `gorm:"not null;index:idx_tg_acct_bot_user"`
	BotUsername          string `gorm:"size:64;index"`
	BotName              string `gorm:"size:255"`
	CanConnectToBusiness bool   `gorm:"not null;default:false"`

	BotToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	WebhookSecret piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	WebhookSetAt        *time.Time `gorm:"type:timestamptz"`
	WebhookPendingCount int        `gorm:"not null;default:0"`
	WebhookLastError    string     `gorm:"size:500"`
	WebhookLastErrorAt  *time.Time `gorm:"type:timestamptz"`
	WebhookCheckedAt    *time.Time `gorm:"type:timestamptz;index:idx_tg_acct_wh_checked"`

	BusinessConnectionID *string        `gorm:"size:128;index:idx_tg_acct_business_conn"`
	BusinessUserID       *int64         `gorm:"index"`
	BusinessUsername     string         `gorm:"size:64"`
	BusinessRights       datatypes.JSON `gorm:"type:jsonb"`
	BusinessEnabled      bool           `gorm:"not null;default:false"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	EnableAutoMemory     bool    `gorm:"not null;default:false"`

	Status       string `gorm:"size:24;not null;default:'PENDING';index"`
	StatusReason string `gorm:"size:255"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_tg_acct_ws_del,priority:2"`
}

func (TelegramAccount) TableName() string { return "telegram_accounts" }

func (a *TelegramAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type TelegramContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_tg_contact_account"`

	TGUserID int64  `gorm:"not null;index:idx_tg_contact_user"`
	TGChatID int64  `gorm:"not null;index"`
	ChatType string `gorm:"size:16;not null;default:'private'"`

	Username         string     `gorm:"size:64;index"`
	FirstName        string     `gorm:"size:128"`
	LastName         string     `gorm:"size:128"`
	LanguageCode     string     `gorm:"size:16"`
	IsPremium        bool       `gorm:"not null;default:false"`
	PhotoFileID      string     `gorm:"size:256"`
	PhotoURL         string     `gorm:"size:1024"`
	ProfileFetchedAt *time.Time `gorm:"type:timestamptz"`

	PhoneNumber   *string    `gorm:"size:32;index"`
	PhoneSharedAt *time.Time `gorm:"type:timestamptz"`
	LeadID        *string    `gorm:"type:uuid;index"`

	Blocked   bool       `gorm:"not null;default:false;index"`
	BlockedAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (TelegramContact) TableName() string { return "telegram_contacts" }

func (c *TelegramContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type TelegramConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_tg_conv_account"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_tg_conv_contact"`

	TGChatID int64  `gorm:"not null;index:idx_tg_conv_chat"`
	ChatType string `gorm:"size:16;not null;default:'private'"`

	BusinessConnectionID *string `gorm:"size:128;index"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_tg_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_tg_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at;index"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

	StartPayload *string `gorm:"size:64;index"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (TelegramConversation) TableName() string { return "telegram_conversations" }

func (c *TelegramConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type TelegramDeepLink struct {
	Token       string `gorm:"primaryKey;size:64"`
	AccountID   string `gorm:"type:uuid;not null;index"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`

	LeadID       *string `gorm:"type:uuid;index"`
	CampaignID   *string `gorm:"type:uuid;index"`
	AgentID      *string `gorm:"type:uuid"`
	DepartmentID *string `gorm:"type:uuid"`
	Label        string  `gorm:"size:255"`

	ExpiresAt *time.Time `gorm:"type:timestamptz;index"`
	UsedAt    *time.Time `gorm:"type:timestamptz"`
	UseCount  int        `gorm:"not null;default:0"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (TelegramDeepLink) TableName() string { return "telegram_deep_links" }

type TelegramFileCache struct {
	ID        string `gorm:"primaryKey;type:uuid"`
	AccountID string `gorm:"type:uuid;not null;index:idx_tg_file_cache,priority:1"`
	SourceKey string `gorm:"size:512;not null;index:idx_tg_file_cache,priority:2"`
	FileID    string `gorm:"size:256;not null"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (TelegramFileCache) TableName() string { return "telegram_file_cache" }

func (f *TelegramFileCache) BeforeCreate(tx *gorm.DB) error {
	if f.ID == "" {
		f.ID = uuid.New().String()
	}
	return nil
}
