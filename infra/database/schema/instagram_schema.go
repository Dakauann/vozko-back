package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type InstagramAccount struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_ig_acct_workspace;index:idx_ig_acct_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`

	IGUserID          string `gorm:"column:ig_user_id;uniqueIndex;size:64;not null"`
	Username          string `gorm:"size:64;index"`
	Name              string `gorm:"size:255"`
	ProfilePictureURL string `gorm:"size:1024"`
	AccountType       string `gorm:"size:32"`
	FollowersCount    int    `gorm:"default:0"`
	FollowsCount      int    `gorm:"default:0"`
	MediaCount        int    `gorm:"default:0"`

	AccessToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	TokenExpiresAt   *time.Time              `gorm:"type:timestamptz;index:idx_ig_acct_token_exp"`
	TokenRefreshedAt *time.Time              `gorm:"type:timestamptz"`
	GrantedScopes    string                  `gorm:"type:text"`

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

	WebhookSubscribedAt *time.Time `gorm:"type:timestamptz"`
	MessagingHealthy    bool       `gorm:"not null;default:false"`
	MessagingCheckedAt  *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_ig_acct_ws_del,priority:2"`
}

func (InstagramAccount) TableName() string { return "instagram_accounts" }

func (a *InstagramAccount) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type InstagramContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_contact_account"`
	IGSID       string `gorm:"column:igsid;size:64;not null;index:idx_ig_contact_igsid"`

	Username             string     `gorm:"size:64;index"`
	Name                 string     `gorm:"size:255"`
	ProfilePictureURL    string     `gorm:"size:1024"`
	IsVerifiedUser       bool       `gorm:"not null;default:false"`
	FollowerCount        int        `gorm:"default:0"`
	IsUserFollowBusiness bool       `gorm:"not null;default:false"`
	IsBusinessFollowUser bool       `gorm:"not null;default:false"`
	ProfileFetchedAt     *time.Time `gorm:"type:timestamptz"`

	LeadID *string `gorm:"type:uuid;index"`

	Blocked   bool           `gorm:"not null;default:false;index"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (InstagramContact) TableName() string { return "instagram_contacts" }

func (c *InstagramContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type InstagramConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_conv_account"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_ig_conv_contact"`

	IGConversationID *string `gorm:"size:128;index"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index:idx_ig_conv_status"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_ig_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_ig_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at;index:idx_ig_conv_lastmsg"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`
	AwaitingReplySince    *time.Time `gorm:"column:awaiting_reply_since"`

	Metadata  LeadMetadata   `gorm:"type:jsonb;default:'{}'"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (InstagramConversation) TableName() string { return "instagram_conversations" }

func (c *InstagramConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type InstagramMedia struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_media_account"`
	IGMediaID   string `gorm:"column:ig_media_id;size:64;not null;uniqueIndex"`

	MediaType        string     `gorm:"size:24"`
	MediaProductType string     `gorm:"size:24;index:idx_ig_media_product"`
	Caption          string     `gorm:"type:text"`
	Permalink        string     `gorm:"size:512"`
	Shortcode        string     `gorm:"size:64"`
	Timestamp        *time.Time `gorm:"type:timestamptz;index:idx_ig_media_ts"`

	LikeCount        int   `gorm:"default:0"`
	CommentsCount    int   `gorm:"default:0"`
	IsCommentEnabled *bool `gorm:"default:null"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (InstagramMedia) TableName() string { return "instagram_media" }

func (m *InstagramMedia) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

type InstagramComment struct {
	ID                string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string  `gorm:"type:uuid;not null;index"`
	IGAccountID       string  `gorm:"type:uuid;not null;index:idx_ig_comment_account"`
	IGCommentID       string  `gorm:"column:ig_comment_id;size:64;not null;uniqueIndex"`
	IGMediaID         string  `gorm:"column:ig_media_id;size:64;not null;index:idx_ig_comment_media"`
	ParentIGCommentID *string `gorm:"column:parent_ig_comment_id;size:64;index"`

	FromIGSID    string `gorm:"column:from_igsid;size:64;index"`
	FromUsername string `gorm:"size:64"`
	Text         string `gorm:"type:text"`
	LikeCount    int    `gorm:"default:0"`
	Hidden       bool   `gorm:"not null;default:false;index:idx_ig_comment_hidden"`
	IsOurs       bool   `gorm:"not null;default:false"`

	Timestamp *time.Time `gorm:"type:timestamptz;index:idx_ig_comment_ts"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (InstagramComment) TableName() string { return "instagram_comments" }

func (c *InstagramComment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type WebhookProcessedEvent struct {
	ID        string    `gorm:"primaryKey;size:255"`
	Channel   string    `gorm:"size:64;not null;index"`
	AccountID string    `gorm:"size:64;index"`
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_webhook_processed_created"`
}

func (WebhookProcessedEvent) TableName() string { return "webhook_processed_events" }
