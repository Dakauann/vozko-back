package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

// InstagramAccount is a connected Instagram professional account. It doubles as
// the config carrier for its conversations — the role whatsapp_campaigns plays
// for WhatsApp — which is why the agent/workflow/automation columns live here.
// Instagram has no campaign analogue: outbound-first messaging is impossible, so
// there is nothing to blast and no template to carry.
type InstagramAccount struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_ig_acct_workspace;index:idx_ig_acct_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`

	// IGUserID is the Instagram professional account ID (`user_id` from GET /me,
	// not the app-scoped `id`). Globally unique, mirroring
	// WhatsAppBusinessPhoneNumber.MetaPhoneNumberID, so the same account cannot
	// be connected to two workspaces.
	IGUserID          string `gorm:"column:ig_user_id;uniqueIndex;size:64;not null"`
	Username          string `gorm:"size:64;index"`
	Name              string `gorm:"size:255"`
	ProfilePictureURL string `gorm:"size:1024"`
	AccountType       string `gorm:"size:32"`
	FollowersCount    int    `gorm:"default:0"`
	FollowsCount      int    `gorm:"default:0"`
	MediaCount        int    `gorm:"default:0"`

	// AccessToken is envelope-encrypted at rest via piigorm, unlike the
	// WhatsApp Meta token which is still stored as plaintext text.
	AccessToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	TokenExpiresAt   *time.Time              `gorm:"type:timestamptz;index:idx_ig_acct_token_exp"`
	TokenRefreshedAt *time.Time              `gorm:"type:timestamptz"`
	// GrantedScopes is the comma-separated list the user ACTUALLY granted;
	// individual scopes can be declined.
	GrantedScopes string `gorm:"type:text"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`

	Status       string `gorm:"size:24;not null;default:'PENDING';index"`
	StatusReason string `gorm:"size:255"`

	WebhookSubscribedAt *time.Time `gorm:"type:timestamptz"`
	// MessagingHealthy tracks the Instagram-app "Allow Access to Messages"
	// toggle. There is no API for that flag, so this is a probe result; when it
	// is off, DMs and messaging webhooks fail silently.
	MessagingHealthy   bool       `gorm:"not null;default:false"`
	MessagingCheckedAt *time.Time `gorm:"type:timestamptz"`

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

// InstagramContact is a person who messaged one of our accounts.
//
// Identity is (ig_account_id, igsid), never IGSID alone: an Instagram-scoped ID
// is scoped to the (app, professional account) pair, so the same human has a
// different IGSID on each connected account. The unique index is created in
// migrate.go because it needs a WHERE clause GORM tags cannot express.
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

	// LeadID optionally bridges to a WhatsApp lead for cross-channel identity.
	// Unused by the base implementation; present so a merge feature needs no
	// migration.
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

// InstagramConversation is the ENTRY — what the CRM treats as a conversation.
// conversation_messages rows point here via (entry_id, entry_type='instagram'),
// and because labels, stages, opportunities and inbox assignment all key on that
// same pair, they work here with no change.
//
// Note the absence of a campaign_id: ig_account_id is the container.
type InstagramConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_conv_account"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_ig_conv_contact"`

	// IGConversationID is Meta's thread id. Nullable because ingest is
	// webhook-first — we learn a thread from a message, not from a sync.
	IGConversationID *string `gorm:"size:128;index"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index:idx_ig_conv_status"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	ClosedAt           *time.Time `gorm:"column:closed_at"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	// Denormalized clocks, same contract as whatsapp_campaign_entries.
	// LastCustomerMessageAt is the anchor for the sliding 24h window.
	LastMessageAt         *time.Time `gorm:"column:last_message_at;index:idx_ig_conv_lastmsg"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

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

// InstagramMedia is the durable projection of a post.
//
// media_url and thumbnail_url are deliberately NOT stored: they are short-lived
// signed CDN links that stop resolving, so they are fetched on demand and served
// through the media proxy. id and permalink are the durable keys.
type InstagramMedia struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_media_account"`
	IGMediaID   string `gorm:"column:ig_media_id;size:64;not null;uniqueIndex"`

	MediaType string `gorm:"size:24"`
	// MediaProductType distinguishes FEED/REELS/STORY/AD. There is no
	// media_type=REELS, so all per-type logic branches on this column.
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

// InstagramComment stores comments so moderation is push-driven from webhooks.
// The Graph comments edge cannot be filtered by timestamp, so incremental sync
// has no other reliable source.
type InstagramComment struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_comment_account"`
	IGCommentID string `gorm:"column:ig_comment_id;size:64;not null;uniqueIndex"`
	IGMediaID   string `gorm:"column:ig_media_id;size:64;not null;index:idx_ig_comment_media"`
	// ParentIGCommentID set means this row is a reply.
	ParentIGCommentID *string `gorm:"column:parent_ig_comment_id;size:64;index"`

	FromIGSID    string `gorm:"column:from_igsid;size:64;index"`
	FromUsername string `gorm:"size:64"`
	Text         string `gorm:"type:text"`
	LikeCount    int    `gorm:"default:0"`
	Hidden       bool   `gorm:"not null;default:false;index:idx_ig_comment_hidden"`
	// IsOurs comes from the presence of Graph's `user` field, which is populated
	// only for comments our own app user authored. It decides whether deletion
	// is possible at all: DELETE requires the comment creator's token.
	IsOurs bool `gorm:"not null;default:false"`

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

// InstagramPrivateReply guards Instagram's one-private-reply-per-comment rule.
//
// The row is written BEFORE the HTTP call and the comment id is the primary key,
// so a retry after an ambiguous timeout cannot burn the single allowance.
type InstagramPrivateReply struct {
	IGCommentID string `gorm:"column:ig_comment_id;primaryKey;size:64"`
	IGAccountID string `gorm:"type:uuid;not null;index"`
	Status      string `gorm:"size:16;not null;default:'ATTEMPTED'"`

	RecipientIGSID *string `gorm:"column:recipient_igsid;size:64"`
	IGMessageID    *string `gorm:"column:ig_message_id;type:text"`
	ErrorCode      int     `gorm:"default:0"`
	ErrorMessage   string  `gorm:"size:500"`

	AttemptedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

func (InstagramPrivateReply) TableName() string { return "instagram_private_replies" }

// WebhookProcessedEvent is the durable webhook dedup store.
//
// The existing WhatsApp pipeline dedups only in Redis with a 5-minute TTL, and
// its one-shot variant fails OPEN on a Redis error, so a duplicate delivery
// after eviction is inserted twice. This table follows the proven
// telemetry_dedupe pattern (INSERT ... ON CONFLICT DO NOTHING) and survives
// eviction and restarts. It is channel-agnostic so other channels can adopt it.
type WebhookProcessedEvent struct {
	// ID is the composite idempotency key, e.g.
	// "ig:<account>:messages:<mid>". A single mid recurs across event kinds
	// (original, tombstone, edit, read, reaction), so the kind is part of it.
	ID string `gorm:"primaryKey;size:255"`
	// 64, not 24: the consumer names are the values stored here, and
	// "instagram-message-webhook" is 25 characters — one over the old limit, which
	// made every durable dedup claim fail with 22001 and silently fall back to the
	// 5-minute Redis guard, losing the durable backstop entirely.
	Channel   string    `gorm:"size:64;not null;index"`
	AccountID string    `gorm:"size:64;index"`
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_webhook_processed_created"`
}

func (WebhookProcessedEvent) TableName() string { return "webhook_processed_events" }

// InstagramCommentRule is one stored comment automation.
//
// A rule scoped to a media id applies to that post only; an empty media id makes
// it an account-wide default. Both tiers are read together and ordered by
// priority, so a post rule can pre-empt a default.
type InstagramCommentRule struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	IGAccountID string `gorm:"type:uuid;not null;index:idx_ig_rule_account"`

	Name    string `gorm:"size:120;not null"`
	Enabled bool   `gorm:"not null;default:true;index:idx_ig_rule_enabled"`

	// IGMediaID empty means "every post on this account".
	IGMediaID string `gorm:"column:ig_media_id;size:64;not null;default:'';index:idx_ig_rule_media"`

	Match    string         `gorm:"size:16;not null;default:'contains'"`
	Keywords pq.StringArray `gorm:"type:text[]"`
	Actions  pq.StringArray `gorm:"type:text[]"`

	PublicReplyText  string `gorm:"type:text"`
	PrivateReplyText string `gorm:"type:text"`

	Priority int `gorm:"not null;default:0;index:idx_ig_rule_priority"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (InstagramCommentRule) TableName() string { return "instagram_comment_rules" }

func (r *InstagramCommentRule) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	return nil
}
