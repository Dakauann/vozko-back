package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type TikTokAccount struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_tt_acct_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`

	OpenID            string `gorm:"column:open_id;size:256;not null;index:idx_tt_acct_open_id"`
	UniqueIdentifier  string `gorm:"size:256;not null;default:''"`
	Username          string `gorm:"size:64;not null;default:''"`
	DisplayName       string `gorm:"size:255;not null;default:''"`
	AvatarKey         string `gorm:"size:512;not null;default:''"`
	HasAdvancedAccess bool   `gorm:"not null;default:false"`
	IsBusinessAccount bool   `gorm:"not null;default:false"`

	AccessToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	RefreshToken     piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	AccessExpiresAt  *time.Time              `gorm:"type:timestamptz"`
	RefreshExpiresAt *time.Time              `gorm:"type:timestamptz"`
	GrantedScopes    string                  `gorm:"type:text;not null;default:''"`

	CanReadVideos         bool `gorm:"not null;default:false"`
	CanReadComments       bool `gorm:"not null;default:false"`
	CanManageComments     bool `gorm:"not null;default:false"`
	CanReceiveMessages    bool `gorm:"not null;default:false"`
	CanSendMessages       bool `gorm:"not null;default:false"`
	CanManageInboxProfile bool `gorm:"not null;default:false"`

	Status       string `gorm:"size:24;not null;default:'DISCONNECTED'"`
	StatusReason string `gorm:"size:255;not null;default:''"`

	WebhooksHealthy   bool       `gorm:"not null;default:false"`
	LastHealthCheckAt *time.Time `gorm:"type:timestamptz"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	EnableAutoMemory     bool    `gorm:"not null;default:false"`
	AutomationDisclosure string  `gorm:"type:text;not null;default:''"`

	PurgeAfter *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_tt_acct_ws_del,priority:2"`
}

func (TikTokAccount) TableName() string { return "tiktok_accounts" }

func (a *TikTokAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type TikTokContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_tt_contact_account"`

	UniqueIdentifier   string     `gorm:"size:256;not null"`
	Username           string     `gorm:"size:64;not null;default:''"`
	DisplayName        string     `gorm:"size:255;not null;default:''"`
	AvatarKey          string     `gorm:"size:512;not null;default:''"`
	IsFollower         *bool      `gorm:"default:null"`
	ProfileRefreshedAt *time.Time `gorm:"type:timestamptz"`

	LeadID *string `gorm:"type:uuid;index"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (TikTokContact) TableName() string { return "tiktok_contacts" }

func (c *TikTokContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type TikTokConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_tt_conv_account"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_tt_conv_contact"`

	ConversationID   string `gorm:"column:conversation_id;size:256;not null"`
	ConversationType string `gorm:"size:16;not null;default:''"`

	OutboundSinceInbound int        `gorm:"not null;default:0"`
	BudgetClosedAt       *time.Time `gorm:"type:timestamptz"`

	ImageSendAllowed   *bool      `gorm:"default:null"`
	ImageSendCheckedAt *time.Time `gorm:"type:timestamptz"`

	Referral datatypes.JSON `gorm:"type:jsonb"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index:idx_tt_conv_status"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_tt_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_tt_close_outcome,priority:1"`
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

func (TikTokConversation) TableName() string { return "tiktok_conversations" }

func (c *TikTokConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type TikTokVideo struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null"`

	ItemID       string `gorm:"size:32;not null"`
	MediaType    string `gorm:"size:8;not null;default:'VIDEO'"`
	Caption      string `gorm:"type:text;not null;default:''"`
	ShareURL     string `gorm:"type:text;not null;default:''"`
	EmbedURL     string `gorm:"type:text;not null;default:''"`
	ThumbnailKey string `gorm:"size:512;not null;default:''"`
	IsAd         bool   `gorm:"not null;default:false"`

	Likes    int64 `gorm:"not null;default:0"`
	Comments int64 `gorm:"not null;default:0"`
	Shares   int64 `gorm:"not null;default:0"`
	Views    int64 `gorm:"not null;default:0"`

	PostedAt *time.Time `gorm:"type:timestamptz"`
	SyncedAt time.Time  `gorm:"type:timestamptz;not null"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (TikTokVideo) TableName() string { return "tiktok_videos" }

func (v *TikTokVideo) BeforeCreate(tx *gorm.DB) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	return nil
}

type TikTokComment struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null"`

	VideoID         string `gorm:"size:32;not null"`
	CommentID       string `gorm:"size:32;not null"`
	ParentCommentID string `gorm:"size:32;not null;default:''"`
	Kind            string `gorm:"size:8;not null;default:'comment'"`

	ContactID              *string `gorm:"type:uuid;index"`
	AuthorUniqueIdentifier string  `gorm:"size:256;not null;default:''"`
	AuthorUsername         string  `gorm:"size:64;not null;default:''"`
	AuthorDisplayName      string  `gorm:"size:255;not null;default:''"`

	Text       string `gorm:"type:text;not null;default:''"`
	ImageURL   string `gorm:"type:text;not null;default:''"`
	Visibility string `gorm:"size:16;not null;default:''"`
	IsOurs     *bool  `gorm:"default:null"`
	LikedByUs  bool   `gorm:"not null;default:false"`
	Likes      int    `gorm:"not null;default:0"`
	Replies    int    `gorm:"not null;default:0"`

	CommentedAt *time.Time `gorm:"type:timestamptz"`
	LastEventAt *time.Time `gorm:"type:timestamptz"`

	Lookup         string     `gorm:"size:12;not null;default:'resolved'"`
	LookupAttempts int        `gorm:"not null;default:0"`
	NextLookupAt   *time.Time `gorm:"type:timestamptz"`
	RulesDue       bool       `gorm:"not null;default:false"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (TikTokComment) TableName() string { return "tiktok_comments" }

func (c *TikTokComment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

func TikTokModels() []any {
	return []any{&TikTokAccount{}, &TikTokContact{}, &TikTokConversation{}, &TikTokVideo{}, &TikTokComment{}}
}
