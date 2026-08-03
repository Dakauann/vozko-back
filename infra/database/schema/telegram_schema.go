package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

// TelegramAccount is a connected Telegram bot. It doubles as the config carrier
// for its conversations, the role whatsapp_campaigns plays for WhatsApp and
// instagram_accounts plays for Instagram, which is why the agent/workflow
// columns live here. Telegram has no campaign analogue: a bot cannot open a
// conversation, so there is nothing to blast and no template to carry.
type TelegramAccount struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_tg_acct_workspace;index:idx_tg_acct_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`

	// Mode is BOT or BUSINESS. They are not variants of one flow: they differ in
	// who owns the chat, how the account is onboarded, how a webhook is routed to
	// it, and whether there is a messaging window.
	Mode string `gorm:"size:16;not null;default:'BOT';index"`

	// BotUserID is the bot's own Telegram id. Telegram ids carry up to 52
	// significant bits, so this is bigint, a 32-bit column would corrupt them
	// silently. Globally unique (enforced by a partial index in migrate.go), so
	// one bot cannot be connected to two workspaces.
	BotUserID   int64  `gorm:"not null;index:idx_tg_acct_bot_user"`
	BotUsername string `gorm:"size:64;index"`
	BotName     string `gorm:"size:255"`
	// CanConnectToBusiness mirrors getMe's flag: whether this bot may be attached
	// to a Telegram Business account at all.
	CanConnectToBusiness bool `gorm:"not null;default:false"`

	// BotToken is envelope-encrypted at rest. A Telegram token never expires and
	// grants total control of the bot, so plaintext storage would be strictly
	// worse than the Meta equivalent, which at least rotates.
	BotToken piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	// WebhookSecret is echoed by Telegram in X-Telegram-Bot-Api-Secret-Token.
	// Telegram does not sign the body, so this header is the ONLY authenticity
	// control the channel has, it is a credential, and encrypted like one.
	WebhookSecret piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	WebhookSetAt *time.Time `gorm:"type:timestamptz"`
	// Webhook health mirrors getWebhookInfo. This is the channel's data-loss
	// alarm, not a cosmetic flag: undelivered updates are discarded after 24
	// hours and there is no history API to recover them from.
	WebhookPendingCount int        `gorm:"not null;default:0"`
	WebhookLastError    string     `gorm:"size:500"`
	WebhookLastErrorAt  *time.Time `gorm:"type:timestamptz"`
	WebhookCheckedAt    *time.Time `gorm:"type:timestamptz;index:idx_tg_acct_wh_checked"`

	// Business-mode identity. Null in bot mode.
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

// TelegramContact is a person who messaged one of our bots.
//
// A Telegram user id is GLOBAL, unlike an Instagram IGSID, the same human has
// the same id for every bot. Identity is still scoped to (account, user) so one
// workspace can never read another's contact row; the unique index is created in
// migrate.go because it needs a WHERE clause GORM tags cannot express.
type TelegramContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_tg_contact_account"`

	TGUserID int64 `gorm:"not null;index:idx_tg_contact_user"`
	// TGChatID equals TGUserID for private chats but is stored explicitly: group
	// chats differ, and a group upgraded to a supergroup gets a NEW id delivered
	// as ResponseParameters.migrate_to_chat_id.
	TGChatID int64  `gorm:"not null;index"`
	ChatType string `gorm:"size:16;not null;default:'private'"`

	Username     string `gorm:"size:64;index"`
	FirstName    string `gorm:"size:128"`
	LastName     string `gorm:"size:128"`
	LanguageCode string `gorm:"size:16"`
	IsPremium    bool   `gorm:"not null;default:false"`
	// PhotoFileID is durable; the URL it resolves to is valid "for at least 1
	// hour", so only the id is stored.
	PhotoFileID      string     `gorm:"size:256"`
	PhotoURL         string     `gorm:"size:1024"`
	ProfileFetchedAt *time.Time `gorm:"type:timestamptz"`

	// PhoneNumber is only ever written from an explicit consent tap on a
	// request_contact button. Telegram never volunteers it.
	PhoneNumber   *string    `gorm:"size:32;index"`
	PhoneSharedAt *time.Time `gorm:"type:timestamptz"`
	// LeadID bridges this contact to a CRM lead once a shared phone makes the
	// match possible.
	LeadID *string `gorm:"type:uuid;index"`

	// Blocked comes from my_chat_member, which in private chats fires only on
	// block/unblock. In bot mode this, not a clock, is what closes the
	// composer.
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

// TelegramConversation is the ENTRY, what the CRM treats as a conversation.
// conversation_messages rows point here via (entry_id, entry_type='telegram'),
// and because labels, stages, opportunities and inbox assignment all key on that
// pair, they work for Telegram with no change.
type TelegramConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;index:idx_tg_conv_account"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_tg_conv_contact"`

	TGChatID int64  `gorm:"not null;index:idx_tg_conv_chat"`
	ChatType string `gorm:"size:16;not null;default:'private'"`

	// BusinessConnectionID records which connection this conversation arrived
	// through, so a reply is sent on behalf of the right account.
	BusinessConnectionID *string `gorm:"size:128;index"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	ClosedAt           *time.Time `gorm:"column:closed_at"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	// Denormalized clocks, same contract as the other channels'.
	// LastCustomerMessageAt is the business-mode 24h window anchor; bot mode has
	// no window and does not read it.
	LastMessageAt         *time.Time `gorm:"column:last_message_at;index"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

	// StartPayload is the deep-link token the contact arrived with.
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

// TelegramDeepLink is a t.me/<bot>?start=<token> attribution record.
//
// Telegram caps the start parameter at 64 characters from a restricted alphabet,
// which is not enough to carry real ids, so the link carries an opaque token
// and this row carries what it resolves to. It is the channel's answer to having
// no cold outbound: a link in an email, an invoice or a QR code opens an
// already-attributed conversation on the customer's first tap.
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
	// UseCount counts redemptions rather than consuming the link: a QR code on a
	// printed invoice is scanned many times and must keep working.
	UseCount int `gorm:"not null;default:0"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (TelegramDeepLink) TableName() string { return "telegram_deep_links" }

// TelegramFileCache maps one of our stored objects to the file_id Telegram
// assigned it.
//
// "There are no limits for files sent this way", so a cached id turns every
// repeat send of the same asset into a free, instant call, and it sidesteps the
// URL-send size caps entirely. The id is per bot: "file_id is unique for each
// individual bot and can't be transferred from one bot to another".
type TelegramFileCache struct {
	ID        string `gorm:"primaryKey;type:uuid"`
	AccountID string `gorm:"type:uuid;not null;index:idx_tg_file_cache,priority:1"`
	// SourceKey is our own object key (the R2 path), not a URL: URLs are signed
	// and rotate, and the same object must map to the same cached id across
	// re-signings.
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
