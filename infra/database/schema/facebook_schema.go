package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type FacebookGrant struct {
	ID               string                  `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string                  `gorm:"type:uuid;not null;index"`
	ConnectedBy      string                  `gorm:"type:uuid;not null"`
	TokenKind        string                  `gorm:"size:16;not null"`
	AccessToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	TokenExpiresAt   *time.Time              `gorm:"type:timestamptz"`
	AppScopedUserID  string                  `gorm:"size:64;not null;index"`
	ClientBusinessID string                  `gorm:"size:64;index"`
	Scopes           string                  `gorm:"type:text"`
	GranularScopes   datatypes.JSON          `gorm:"type:jsonb;default:'{}'"`
	Status           string                  `gorm:"size:16;not null;default:'ACTIVE';index"`
	CheckedAt        *time.Time              `gorm:"type:timestamptz"`
	RevokedAt        *time.Time              `gorm:"type:timestamptz"`
	CreatedAt        time.Time               `gorm:"autoCreateTime"`
	UpdatedAt        time.Time               `gorm:"autoUpdateTime"`
	DeletedAt        gorm.DeletedAt          `gorm:"index"`
}

func (FacebookGrant) TableName() string { return "facebook_grants" }

func (g *FacebookGrant) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return nil
}

type FacebookPage struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index;index:idx_fb_page_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	GrantID      string  `gorm:"type:uuid;not null;index"`

	FBPageID          string `gorm:"column:fb_page_id;size:64;not null;uniqueIndex"`
	Name              string `gorm:"size:255"`
	Username          string `gorm:"size:128"`
	Category          string `gorm:"size:128"`
	Link              string `gorm:"size:512"`
	PictureStorageKey string `gorm:"size:512"`
	FollowersCount    int    `gorm:"default:0"`
	LinkedIGUserID    string `gorm:"column:linked_ig_user_id;size:64"`

	PageToken     piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	Tasks         string                  `gorm:"type:text"`
	GrantedScopes string                  `gorm:"type:text"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	EnableAutoMemory     bool    `gorm:"not null;default:false"`
	AutomationDisclosure string  `gorm:"type:text"`

	Status       string `gorm:"size:24;not null;default:'PENDING';index"`
	StatusReason string `gorm:"size:255"`

	SubscribedFields    string     `gorm:"type:text"`
	WebhookSubscribedAt *time.Time `gorm:"type:timestamptz"`
	IsDefaultRouteApp   *bool      `gorm:"default:null"`
	RoutingCheckedAt    *time.Time `gorm:"type:timestamptz"`
	PolicyAction        string     `gorm:"size:16"`
	PolicyReason        string     `gorm:"type:text"`
	PolicyAt            *time.Time `gorm:"type:timestamptz"`
	HealthCheckedAt     *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_fb_page_ws_del,priority:2"`
}

func (FacebookPage) TableName() string { return "facebook_pages" }

func (p *FacebookPage) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

type FacebookContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	PageID      string `gorm:"type:uuid;not null;index:idx_fb_contact_page"`
	PSID        string `gorm:"column:psid;size:64;not null;index:idx_fb_contact_psid"`

	Name              string     `gorm:"size:255"`
	FirstName         string     `gorm:"size:128"`
	LastName          string     `gorm:"size:128"`
	AvatarStorageKey  string     `gorm:"size:512"`
	ProfileStatus     string     `gorm:"size:16;not null;default:'UNKNOWN'"`
	ProfileFetchedAt  *time.Time `gorm:"type:timestamptz"`
	Unreachable       bool       `gorm:"not null;default:false"`
	UnreachableReason string     `gorm:"size:64"`

	LeadID  *string `gorm:"type:uuid;index"`
	Blocked bool    `gorm:"not null;default:false;index"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (FacebookContact) TableName() string { return "facebook_contacts" }

func (c *FacebookContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type FacebookConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	PageID      string `gorm:"type:uuid;not null;index:idx_fb_conv_page"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_fb_conv_contact"`

	FBConversationID *string `gorm:"column:fb_conversation_id;size:128;index"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index:idx_fb_conv_status"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_fb_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_fb_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at;index:idx_fb_conv_lastmsg"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

	DeliveredWatermark *time.Time `gorm:"type:timestamptz"`
	ReadWatermark      *time.Time `gorm:"type:timestamptz"`

	ThreadOwnerAppID  string     `gorm:"size:32"`
	ThreadOwnerSeenAt *time.Time `gorm:"type:timestamptz"`

	Metadata  LeadMetadata   `gorm:"type:jsonb;default:'{}'"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (FacebookConversation) TableName() string { return "facebook_conversations" }

func (c *FacebookConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

type FacebookPost struct {
	ID                   string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID          string     `gorm:"type:uuid;not null;index"`
	PageID               string     `gorm:"type:uuid;not null;index:idx_fb_post_page"`
	FBPostID             string     `gorm:"column:fb_post_id;size:96;not null;uniqueIndex"`
	Kind                 string     `gorm:"size:16;not null;index"`
	StatusType           string     `gorm:"size:48"`
	Message              string     `gorm:"type:text"`
	PermalinkURL         string     `gorm:"size:1024"`
	IsPublished          bool       `gorm:"not null;default:true"`
	ScheduledPublishTime *time.Time `gorm:"type:timestamptz"`
	IsHidden             bool       `gorm:"not null;default:false"`
	CreatedByApp         bool       `gorm:"not null;default:false"`
	ReactionsCount       int        `gorm:"default:0"`
	CommentsCount        int        `gorm:"default:0"`
	SharesCount          int        `gorm:"default:0"`
	CreatedTime          *time.Time `gorm:"type:timestamptz;index:idx_fb_post_created"`
	UpdatedTime          *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (FacebookPost) TableName() string { return "facebook_posts" }

func (p *FacebookPost) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

type FacebookPublishJob struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string         `gorm:"type:uuid;not null;index"`
	PageID       string         `gorm:"type:uuid;not null;index"`
	RequestedBy  string         `gorm:"type:uuid;not null"`
	Kind         string         `gorm:"size:16;not null"`
	Request      datatypes.JSON `gorm:"type:jsonb;not null"`
	Progress     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	Status       string         `gorm:"size:16;not null;index"`
	FBObjectID   string         `gorm:"column:fb_object_id;size:96;index"`
	FBPostID     string         `gorm:"column:fb_post_id;size:96;index"`
	ErrorCode    int
	ErrorSubcode int
	ErrorMessage string     `gorm:"size:500"`
	Ambiguous    bool       `gorm:"not null;default:false"`
	Attempts     int        `gorm:"not null;default:0"`
	NextCheckAt  *time.Time `gorm:"type:timestamptz;index"`
	CreatedAt    time.Time  `gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime;index"`
}

func (FacebookPublishJob) TableName() string { return "facebook_publish_jobs" }

func (j *FacebookPublishJob) BeforeCreate(tx *gorm.DB) error {
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	return nil
}

type FacebookComment struct {
	ID                string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string     `gorm:"type:uuid;not null;index"`
	PageID            string     `gorm:"type:uuid;not null;index:idx_fb_comment_page"`
	FBCommentID       string     `gorm:"column:fb_comment_id;size:96;not null;uniqueIndex"`
	FBPostID          string     `gorm:"column:fb_post_id;size:96;not null;index:idx_fb_comment_post"`
	ParentFBCommentID *string    `gorm:"column:parent_fb_comment_id;size:96;index"`
	FromID            *string    `gorm:"size:64;index"`
	FromName          string     `gorm:"size:255"`
	FromIsPage        bool       `gorm:"not null;default:false"`
	ContactID         *string    `gorm:"type:uuid;index"`
	Message           string     `gorm:"type:text"`
	AttachmentType    string     `gorm:"size:32"`
	LikeCount         int        `gorm:"default:0"`
	ReplyCount        int        `gorm:"default:0"`
	IsHidden          bool       `gorm:"not null;default:false;index:idx_fb_comment_hidden"`
	IsOurs            bool       `gorm:"not null;default:false"`
	LikedByPage       bool       `gorm:"not null;default:false"`
	CreatedTime       *time.Time `gorm:"type:timestamptz;index:idx_fb_comment_ts"`
	EditedAt          *time.Time `gorm:"type:timestamptz"`
	RemovedAt         *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (FacebookComment) TableName() string { return "facebook_comments" }

func (c *FacebookComment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
