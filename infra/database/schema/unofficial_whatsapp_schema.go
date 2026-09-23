package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/infra/crypto/piigorm"
)

type UnofficialWhatsAppServer struct {
	ID          string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID *string `gorm:"type:uuid;index"`

	Name       string                  `gorm:"size:120;not null"`
	Provider   string                  `gorm:"size:32;not null;default:'uazapi';index"`
	BaseURL    string                  `gorm:"size:255;not null;uniqueIndex"`
	AdminToken piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	Capacity int  `gorm:"not null;default:0"`
	InUse    int  `gorm:"not null;default:0"`
	Enabled  bool `gorm:"not null;default:true;index"`
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

type UnofficialWhatsAppInstance struct {
	ID           string  `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string  `gorm:"type:uuid;not null;index:idx_uw_inst_workspace;index:idx_uw_inst_ws_del,priority:1"`
	DepartmentID *string `gorm:"type:uuid;index"`
	ServerID     string  `gorm:"type:uuid;not null;index:idx_uw_inst_server"`
	Provider     string  `gorm:"size:32;not null;default:'uazapi';index"`

	ProviderInstanceID string                  `gorm:"size:64;not null;index:idx_uw_inst_provider_id"`
	ProviderName       string                  `gorm:"size:120"`
	InstanceToken      piigorm.EncryptedString `gorm:"type:bytea" json:"-"`

	DeliveryToken     piigorm.EncryptedString `gorm:"type:bytea" json:"-"`
	DeliveryTokenHash string                  `gorm:"size:64;not null;uniqueIndex:ux_uw_inst_delivery_hash"`
	WebhookSetAt      *time.Time              `gorm:"type:timestamptz"`

	JID            string `gorm:"column:jid;size:64;index:idx_uw_inst_jid"`
	LID            string `gorm:"column:lid;size:64;index"`
	PhoneNumber    string `gorm:"size:32;index"`
	ProfileName    string `gorm:"size:255"`
	ProfilePicURL  string `gorm:"size:1024"`
	IsBusinessAcct bool   `gorm:"not null;default:false"`
	Platform       string `gorm:"size:32"`
	DisplayName    string `gorm:"size:120"`

	Status       string `gorm:"size:24;not null;default:'PROVISIONING';index"`
	StatusReason string `gorm:"size:255"`

	ConnectedAt          *time.Time `gorm:"type:timestamptz"`
	LastDisconnectAt     *time.Time `gorm:"type:timestamptz"`
	LastDisconnectReason string     `gorm:"size:255"`
	LastPolledAt         *time.Time `gorm:"type:timestamptz;index:idx_uw_inst_polled"`

	RestrictionCanSendNew *bool      `gorm:"default:null"`
	RestrictionKey        string     `gorm:"size:64"`
	RestrictionMessage    string     `gorm:"size:500"`
	RestrictionUntil      *time.Time `gorm:"type:timestamptz"`
	RestrictionUsedQuota  int        `gorm:"not null;default:0"`
	RestrictionTotalQuota int        `gorm:"not null;default:0"`
	RestrictionCheckedAt  *time.Time `gorm:"type:timestamptz"`

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
	EnableAutoMemory     bool    `gorm:"not null;default:false"`
	HandleGroups         bool    `gorm:"not null;default:false"`

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

type UnofficialWhatsAppContact struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	InstanceID  string `gorm:"type:uuid;not null;index:idx_uw_contact_instance"`

	JID         string  `gorm:"column:jid;size:64;not null;index:idx_uw_contact_jid"`
	LID         string  `gorm:"column:lid;size:64;index:idx_uw_contact_lid"`
	IsGroup     bool    `gorm:"not null;default:false;index"`
	PhoneNumber string  `gorm:"size:32;index"`
	LeadID      *string `gorm:"type:uuid;index"`

	Name             string     `gorm:"size:255"`
	ContactName      string     `gorm:"size:255"`
	VerifiedName     string     `gorm:"size:255"`
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
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:idx_uw_close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:idx_uw_close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

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

type UnofficialWhatsAppGroup struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	InstanceID  string `gorm:"type:uuid;not null;index:idx_uw_group_instance"`

	JID string `gorm:"column:jid;size:64;not null;index:idx_uw_group_jid"`

	Subject     string `gorm:"size:255"`
	Description string `gorm:"size:1024"`
	OwnerJID    string `gorm:"column:owner_jid;size:64"`

	Announce         bool `gorm:"not null;default:false"`
	Locked           bool `gorm:"not null;default:false"`
	JoinApproval     bool `gorm:"not null;default:false"`
	Ephemeral        bool `gorm:"not null;default:false"`
	DisappearingSecs int  `gorm:"not null;default:0"`
	Community        bool `gorm:"not null;default:false"`

	WeAreAdmin bool `gorm:"not null;default:false"`
	WeCanSend  bool `gorm:"not null;default:true"`

	ParticipantCount int        `gorm:"not null;default:0"`
	GroupCreatedAt   *time.Time `gorm:"type:timestamptz"`

	SyncedAt *time.Time `gorm:"type:timestamptz;index"`
	StaleAt  *time.Time `gorm:"type:timestamptz"`

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

type UnofficialWhatsAppGroupParticipant struct {
	ID      string `gorm:"primaryKey;type:uuid"`
	GroupID string `gorm:"type:uuid;not null;index:idx_uw_gp_group"`

	JID         string  `gorm:"column:jid;size:64;not null"`
	LID         string  `gorm:"column:lid;size:64"`
	PhoneNumber string  `gorm:"size:32;index"`
	DisplayName string  `gorm:"size:255"`
	Role        string  `gorm:"size:16;not null;default:'member'"`
	ContactID   *string `gorm:"type:uuid;index"`

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
