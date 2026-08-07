package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

// UnofficialWhatsAppServer is one provider host we place instances on.
//
// A host is a row rather than a config string because it has a hard ceiling on
// connected instances and refuses past it. Capacity that is not modelled is
// capacity that fails at the moment a customer clicks Connect, with an error no
// support agent can explain.
type UnofficialWhatsAppServer struct {
	ID string `gorm:"primaryKey;type:uuid"`
	// WorkspaceID is NULL for a platform-owned host usable by any workspace, and
	// set for a bring-your-own host. The admin token is host-wide, so a tenant
	// must never be pointed at ours and we must never hold theirs.
	WorkspaceID *string `gorm:"type:uuid;index"`

	Name     string `gorm:"size:120;not null"`
	Provider string `gorm:"size:32;not null;default:'uazapi';index"`
	BaseURL  string `gorm:"size:255;not null;uniqueIndex"`
	// AdminToken can create and delete every instance on the host, so it is
	// envelope-encrypted at rest and never serialized.
	AdminToken piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	// Capacity is OUR bookkeeping, reconciled against the host's own instance
	// list by the health cron. Zero means "unknown" and is treated as full: a
	// wrong guess of "unlimited" keeps placing instances onto a host that has
	// already started refusing them.
	Capacity int  `gorm:"not null;default:0"`
	InUse    int  `gorm:"not null;default:0"`
	Enabled  bool `gorm:"not null;default:true;index"`
	// Draining stops new placements without evicting live sessions, so a host
	// can be retired without disconnecting customers.
	Draining bool `gorm:"not null;default:false"`

	LastHealthyAt *time.Time `gorm:"type:timestamptz"`
	LastError     string     `gorm:"size:500"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UnofficialWhatsAppServer) TableName() string { return "unofficial_whatsapp_servers" }

func (s *UnofficialWhatsAppServer) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// UnofficialWhatsAppInstance is one connected WhatsApp number.
//
// It doubles as the config carrier for its conversations, the role
// whatsapp_campaigns plays for the Cloud API and telegram_accounts plays for
// Telegram, which is why the agent/workflow columns live here.
type UnofficialWhatsAppInstance struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_uw_inst_workspace;index:idx_uw_inst_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	ServerID     string  `gorm:"type:uuid;not null;index:idx_uw_inst_server"`
	// Provider is the vendor behind this instance. A column rather than an
	// assumption, so a second vendor is another adapter behind the same port
	// instead of another entry type and another pass over every CRM registry.
	Provider string `gorm:"size:32;not null;default:'uazapi';index"`

	// ProviderInstanceID is the host's own id, and the value an inbound webhook
	// body carries. It is the second factor that makes forging an event require
	// more than the delivery URL.
	ProviderInstanceID string `gorm:"size:64;not null;index:idx_uw_inst_provider_id"`
	ProviderName       string `gorm:"size:120"`
	// InstanceToken never expires and grants full control of the customer's
	// WhatsApp, so it is envelope-encrypted and never returned by any endpoint.
	InstanceToken piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	// DeliveryToken is the unguessable path segment of our webhook URL, and the
	// ONLY authenticity control this channel has: the provider signs nothing and
	// accepts no header of ours. Encrypted so it can be re-displayed on demand;
	// resolved through the digest below so a dumped row yields no working URL.
	DeliveryToken     piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	DeliveryTokenHash string                  `gorm:"size:64;not null;uniqueIndex:ux_uw_inst_delivery_hash"`
	WebhookSetAt      *time.Time              `gorm:"type:timestamptz"`

	// The connected WhatsApp account. JID is both the operator-facing identity
	// and the uniqueness key: one number cannot be connected to two workspaces.
	//
	// The explicit column names are load-bearing, not style. GORM's naming
	// strategy runs a common-initialism replacer before snake-casing, and `JID`
	// matches its `ID` entry — so the derived column is `j_id`, not `jid`. Every
	// partial index and raw query in this channel says `jid`, and the mismatch
	// surfaces as "column does not exist" during migration. Same for `LID`.
	JID            string `gorm:"column:jid;size:64;index:idx_uw_inst_jid"`
	LID            string `gorm:"column:lid;size:64;index"`
	PhoneNumber    string `gorm:"size:32;index"`
	ProfileName    string `gorm:"size:255"`
	ProfilePicURL  string `gorm:"size:1024"`
	IsBusinessAcct bool   `gorm:"not null;default:false"`
	Platform       string `gorm:"size:32"`
	DisplayName    string `gorm:"size:120"`

	// PROVISIONING | AWAITING_SCAN | CONNECTED | HIBERNATED | DISCONNECTED
	//              | BANNED | PROVISION_FAILED
	Status       string `gorm:"size:24;not null;default:'PROVISIONING';index"`
	StatusReason string `gorm:"size:255"`

	ConnectedAt          *time.Time `gorm:"type:timestamptz"`
	LastDisconnectAt     *time.Time `gorm:"type:timestamptz"`
	LastDisconnectReason string     `gorm:"size:255"`
	// LastPolledAt is when the health cron last reconciled with the host. A
	// stale value IS the alarm: this channel fails silently, and "quiet inbox"
	// and "the session died an hour ago" are indistinguishable without it.
	LastPolledAt *time.Time `gorm:"type:timestamptz;index:idx_uw_inst_polled"`

	// WhatsApp's own restriction state, cached from the provider's diagnostics.
	// Every send reads these; they are not decoration. RestrictionCanSendNew is
	// a pointer because an incomplete diagnosis must not read as permission to
	// keep sending.
	RestrictionCanSendNew *bool      `gorm:"default:null"`
	RestrictionKey        string     `gorm:"size:64"`
	RestrictionMessage    string     `gorm:"size:500"`
	RestrictionUntil      *time.Time `gorm:"type:timestamptz"`
	RestrictionUsedQuota  int        `gorm:"not null;default:0"`
	RestrictionTotalQuota int        `gorm:"not null;default:0"`
	RestrictionCheckedAt  *time.Time `gorm:"type:timestamptz"`

	// Ban-risk controls. Nothing in the provider's API offers these; they are
	// ours, and they are the highest-value protection the channel has.
	DailySendCap    int        `gorm:"not null;default:0"`
	WarmupStartedAt *time.Time `gorm:"type:timestamptz"`
	SendDelayMinMS  int        `gorm:"not null;default:3000"`
	SendDelayMaxMS  int        `gorm:"not null;default:12000"`
	AutoRejectCalls bool       `gorm:"not null;default:false"`

	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	PipelineID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	// HandleGroups is off by default and is a decision, not an oversight: a
	// group conversation is stored and visible but runs no automation.
	HandleGroups bool `gorm:"not null;default:false"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index;index:idx_uw_inst_ws_del,priority:2"`
}

func (UnofficialWhatsAppInstance) TableName() string { return "unofficial_whatsapp_instances" }

func (i *UnofficialWhatsAppInstance) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}

// UnofficialWhatsAppContact is the SUBJECT of a conversation: a person on the
// other side of one of our numbers, or a group.
//
// Groups share this table because every conversation surface asks the same four
// things of a subject — name, handle, picture, blocked — and a group answers all
// four. What only a group has (roster, admin rules, subject history) lives in
// UnofficialWhatsAppGroup, which the inbox query never joins. Giving groups
// their own subject table would have forced a polymorphic key or a UNION into
// the hottest read path in the product.
//
// WhatsApp addresses the same human by two identifiers, a phone-number JID and a
// LID. Both are stored: treating them as two people splits one real chat into
// two CRM conversations, which reads to an operator as data loss rather than as
// a bug. The unique index lives in indexes.go because it needs a WHERE clause
// GORM tags cannot express.
type UnofficialWhatsAppContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	InstanceID  string `gorm:"type:uuid;not null;index:idx_uw_contact_instance"`

	// Explicit column names for the same reason as on the instance: GORM would
	// otherwise derive `j_id` and `l_id` from these all-caps names.
	JID string `gorm:"column:jid;size:64;not null;index:idx_uw_contact_jid"`
	LID string `gorm:"column:lid;size:64;index:idx_uw_contact_lid"`
	// IsGroup marks this subject as a group rather than a person.
	//
	// A stored column rather than a suffix check on the JID: every person-only
	// path (lead bridging, the dialer, broadcast targeting) reads one predicate
	// instead of re-deriving it, and a predicate re-derived at four call sites
	// is one that will be missed at the fifth.
	IsGroup bool `gorm:"not null;default:false;index"`
	// PhoneNumber is E.164 digits with no leading +, and is EMPTY for a group.
	// It is the CRM bridge, and the reason this channel's contacts are
	// first-class where Instagram's and Telegram's are not: a lead, a boleto and
	// the dialer can all address it — which is exactly why a group id must never
	// be written here.
	PhoneNumber string `gorm:"size:32;index"`
	// LeadID links this contact to the CRM lead it is. Always NULL for a group.
	LeadID *string `gorm:"type:uuid;index"`

	Name         string `gorm:"size:255"`
	ContactName  string `gorm:"size:255"`
	VerifiedName string `gorm:"size:255"`
	// PictureURL is our re-hosted copy; PictureSourceURL is the provider url it
	// was made from, kept as the change detector. An unchanged source url means
	// an unchanged photo, which is what lets a refresh skip the download.
	PictureURL       string     `gorm:"size:1024"`
	PictureSourceURL string     `gorm:"size:1024"`
	IsBusiness       bool       `gorm:"not null;default:false"`
	ProfileFetchedAt *time.Time `gorm:"type:timestamptz"`

	Blocked   bool       `gorm:"not null;default:false;index"`
	BlockedAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UnofficialWhatsAppContact) TableName() string { return "unofficial_whatsapp_contacts" }

func (c *UnofficialWhatsAppContact) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// UnofficialWhatsAppConversation is the ENTRY, what the CRM treats as a
// conversation. conversation_messages rows point here via
// (entry_id, entry_type='unofficial_whatsapp'), and because labels, stages,
// opportunities and inbox assignment all key on that pair, they work here with
// no change to those subsystems.
//
// One real WhatsApp chat is one conversation, always — a deliberate divergence
// from the Cloud API channel, where the entry is (campaign, lead). An inbound
// event here carries no campaign, so it could not choose between two entries,
// and the operator would see one chat on their phone and two in the CRM.
type UnofficialWhatsAppConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	InstanceID  string `gorm:"type:uuid;not null;index:idx_uw_conv_instance"`
	ContactID   string `gorm:"type:uuid;not null;index:idx_uw_conv_contact"`

	ChatID  string `gorm:"size:64;not null;index:idx_uw_conv_chat"`
	IsGroup bool   `gorm:"not null;default:false;index"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	ClosedAt           *time.Time `gorm:"column:closed_at"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	// Denormalized clocks, same contract as the other channels'. There is no
	// messaging window on this channel, so LastCustomerMessageAt drives inbox
	// ordering and analysis rather than gating the composer.
	LastMessageAt         *time.Time `gorm:"column:last_message_at;index"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UnofficialWhatsAppConversation) TableName() string {
	return "unofficial_whatsapp_conversations"
}

func (c *UnofficialWhatsAppConversation) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// UnofficialWhatsAppGroup is the cached metadata of one WhatsApp group.
//
// Keyed by (instance, jid) and NOT by conversation, because the two have
// different lifetimes: a number can belong to a group that has never spoken, and
// a conversation only exists once a message arrives. The JID joins them.
//
// Every column here is a CACHE of an explicit provider read. The provider offers
// no push we can trust — its `groups` webhook payload is documented only as "a
// map, the shape varies" — so freshness is a staleness clock plus an
// invalidation flag, never a subscription.
type UnofficialWhatsAppGroup struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	InstanceID  string `gorm:"type:uuid;not null;index:idx_uw_group_instance"`

	JID string `gorm:"column:jid;size:64;not null;index:idx_uw_group_jid"`

	Subject     string `gorm:"size:255"`
	Description string `gorm:"size:1024"`
	OwnerJID    string `gorm:"column:owner_jid;size:64"`

	// Announce restricts posting to admins; the composer reads it so a member
	// is told they cannot post instead of discovering it from a failed send.
	Announce bool `gorm:"not null;default:false"`
	// Locked restricts editing the group's name/picture/topic to admins.
	Locked           bool `gorm:"not null;default:false"`
	JoinApproval     bool `gorm:"not null;default:false"`
	Ephemeral        bool `gorm:"not null;default:false"`
	DisappearingSecs int  `gorm:"not null;default:0"`
	Community        bool `gorm:"not null;default:false"`

	// WeAreAdmin and WeCanSend describe OUR connected number in this group, not
	// the group in the abstract. Every admin control in the UI is gated on them.
	WeAreAdmin bool `gorm:"not null;default:false"`
	WeCanSend  bool `gorm:"not null;default:true"`

	ParticipantCount int        `gorm:"not null;default:0"`
	GroupCreatedAt   *time.Time `gorm:"type:timestamptz"`

	// SyncedAt is when the provider last confirmed everything above. NULL means
	// never, which reads as stale rather than as fresh.
	SyncedAt *time.Time `gorm:"type:timestamptz;index"`
	// StaleAt is set by the `groups` webhook. A StaleAt later than SyncedAt
	// means "re-read before trusting this row" — which is the only honest thing
	// an unspecified payload can tell us.
	StaleAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UnofficialWhatsAppGroup) TableName() string { return "unofficial_whatsapp_groups" }

func (g *UnofficialWhatsAppGroup) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return nil
}

// UnofficialWhatsAppGroupParticipant is one member, as of the last roster sync.
//
// Members are NOT contacts. A 200-member group would otherwise put 200 rows into
// the table the dialer and the lead bridge read, for people who have never
// messaged the business and cannot be attended. ContactID links the ones we do
// know from a direct chat, and is legitimately NULL for everyone else.
type UnofficialWhatsAppGroupParticipant struct {
	ID      string `gorm:"primaryKey;type:uuid"`
	GroupID string `gorm:"type:uuid;not null;index:idx_uw_gp_group"`

	JID         string `gorm:"column:jid;size:64;not null"`
	LID         string `gorm:"column:lid;size:64"`
	PhoneNumber string `gorm:"size:32;index"`
	DisplayName string `gorm:"size:255"`
	// Role is member | admin | superadmin.
	Role      string  `gorm:"size:16;not null;default:'member'"`
	ContactID *string `gorm:"type:uuid;index"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (UnofficialWhatsAppGroupParticipant) TableName() string {
	return "unofficial_whatsapp_group_participants"
}

func (p *UnofficialWhatsAppGroupParticipant) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
