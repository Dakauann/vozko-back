package schema

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ChannelAutomationColumns struct {
	AgentID              *string `gorm:"type:uuid;index"`
	WorkflowID           *string `gorm:"type:uuid;index"`
	EnableAgentResponses bool    `gorm:"not null;default:false"`
	EnableWorkflow       bool    `gorm:"not null;default:false"`
	EnableAnalysis       bool    `gorm:"not null;default:false"`
	EnableAutoStaging    bool    `gorm:"not null;default:false"`
	EnableAutoMemory     bool    `gorm:"not null;default:false"`
	AutomationDisclosure string  `gorm:"type:text;not null;default:''"`
}

type ChannelContainerColumns struct {
	DepartmentID *string `gorm:"type:uuid;index"`
	PipelineID   *string `gorm:"type:uuid;index"`
}

type RCSSender struct {
	ID                       string `gorm:"primaryKey;type:uuid"`
	WorkspaceID              string `gorm:"type:uuid;not null;index:idx_rcs_senders_workspace,priority:1"`
	BrandName                string `gorm:"type:varchar(80);not null;default:''"`
	Description              string `gorm:"type:varchar(255);not null;default:''"`
	LogoURL                  string `gorm:"type:text;not null;default:''"`
	BannerURL                string `gorm:"type:text;not null;default:''"`
	Color                    string `gorm:"type:varchar(7);not null;default:''"`
	UseCase                  string `gorm:"type:varchar(24);not null;default:''"`
	Sector                   string `gorm:"type:varchar(24);not null;default:''"`
	GamblingAuthorizationURL string `gorm:"type:text;not null;default:''"`
	ContactName              string `gorm:"type:varchar(255);not null;default:''"`
	ContactEmail             string `gorm:"type:varchar(254);not null;default:''"`
	ContactPhone             string `gorm:"type:varchar(32);not null;default:''"`
	WebsiteURL               string `gorm:"type:text;not null;default:''"`
	PrivacyURL               string `gorm:"type:text;not null;default:''"`
	TermsURL                 string `gorm:"type:text;not null;default:''"`
	CNPJ                     string `gorm:"column:cnpj;type:varchar(14);not null;default:''"`
	BillingCategory          string `gorm:"type:varchar(24);not null;default:''"`
	Status                   string `gorm:"type:varchar(24);not null;index:idx_rcs_senders_status"`
	StatusReason             string `gorm:"type:text;not null;default:''"`
	PausedBy                 string `gorm:"type:varchar(16);not null;default:''"`
	PauseReason              string `gorm:"type:varchar(24);not null;default:''"`
	Provider                 string `gorm:"type:varchar(32);not null;default:'';index:idx_rcs_senders_provider_profile,priority:1"`
	ProviderProfileID        string `gorm:"type:varchar(255);not null;default:'';index:idx_rcs_senders_provider_profile,priority:2"`
	ProviderRequestID        string `gorm:"type:varchar(255);not null;default:''"`
	ChannelAutomationColumns
	ChannelContainerColumns
	SubmittedAt *time.Time     `gorm:"type:timestamptz"`
	LaunchedAt  *time.Time     `gorm:"type:timestamptz"`
	PausedAt    *time.Time     `gorm:"type:timestamptz"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;index:idx_rcs_senders_workspace,priority:2"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (RCSSender) TableName() string { return "rcs_senders" }

type CarrierContact struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;uniqueIndex:ux_carrier_contacts_workspace_phone,priority:1,where:deleted_at IS NULL"`
	Phone       string         `gorm:"type:varchar(20);not null;uniqueIndex:ux_carrier_contacts_workspace_phone,priority:2,where:deleted_at IS NULL"`
	DisplayName string         `gorm:"type:varchar(255);not null;default:''"`
	LeadID      *string        `gorm:"type:uuid;index"`
	FloodingAt  *time.Time     `gorm:"type:timestamptz"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (CarrierContact) TableName() string { return "carrier_contacts" }

type CarrierConversation struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;uniqueIndex:,composite:thread,priority:1,where:deleted_at IS NULL;index:,composite:workspace_lastmsg,priority:1"`
	SenderID    string `gorm:"type:uuid;not null;uniqueIndex:,composite:thread,priority:2,where:deleted_at IS NULL;index:,composite:sender"`
	ContactID   string `gorm:"type:uuid;not null;uniqueIndex:,composite:thread,priority:3,where:deleted_at IS NULL;index:,composite:contact"`

	ConversationStatus string     `gorm:"size:20;not null;default:'';index"`
	CloseSource        string     `gorm:"size:20"`
	CloseReason        string     `gorm:"size:40"`
	CloseOutcome       string     `gorm:"size:64;not null;default:'';index:,composite:close_outcome,priority:2"`
	ClosedAt           *time.Time `gorm:"column:closed_at;index:,composite:close_outcome,priority:1"`
	AutomationEnabled  *bool      `gorm:"default:null"`

	LastMessageAt         *time.Time `gorm:"column:last_message_at;index:,composite:workspace_lastmsg,priority:2,sort:desc"`
	LastCustomerMessageAt *time.Time `gorm:"column:last_customer_message_at"`
	LastAgentMessageAt    *time.Time `gorm:"column:last_agent_message_at"`
	AwaitingReplySince    *time.Time `gorm:"column:awaiting_reply_since"`
	ConversationOpenedAt  *time.Time `gorm:"column:conversation_opened_at;type:timestamptz"`

	CreatedAt time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type RCSConversation struct {
	CarrierConversation
}

func (RCSConversation) TableName() string { return "rcs_conversations" }

type SMSConversation struct {
	CarrierConversation
}

func (SMSConversation) TableName() string { return "sms_conversations" }

type SMSRoute struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_sms_routes_workspace,priority:1"`
	BrandPrefix string `gorm:"type:varchar(20);not null"`
	Kind        string `gorm:"type:varchar(16);not null"`
	Number      string `gorm:"type:varchar(15);not null;default:'';uniqueIndex:ux_sms_routes_dedicated_number,where:kind = 'dedicated' AND deleted_at IS NULL"`
	Status      string `gorm:"type:varchar(16);not null"`
	PauseReason string `gorm:"type:varchar(64);not null;default:''"`
	ChannelAutomationColumns
	ChannelContainerColumns
	CreatedAt time.Time      `gorm:"type:timestamptz;not null;index:idx_sms_routes_workspace,priority:2"`
	UpdatedAt time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (SMSRoute) TableName() string { return "sms_routes" }

type CarrierMessage struct {
	ID                string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string         `gorm:"type:uuid;not null;index:idx_carrier_messages_cap,priority:1;index:idx_carrier_messages_workspace_run,priority:1"`
	Channel           string         `gorm:"type:varchar(8);not null;index:idx_carrier_messages_channel"`
	Reference         string         `gorm:"type:varchar(32);not null;uniqueIndex:ux_carrier_messages_reference"`
	SenderID          string         `gorm:"type:uuid;not null;index:idx_carrier_messages_sender"`
	Phone             string         `gorm:"type:varchar(20);not null;index:idx_carrier_messages_cap,priority:2;index:idx_carrier_messages_sms_phone,priority:1,where:step = 'sms'"`
	LeadID            *string        `gorm:"type:uuid;index:idx_carrier_messages_lead"`
	ConversationID    *string        `gorm:"type:uuid;index:idx_carrier_messages_conversation"`
	RunKind           string         `gorm:"type:varchar(16);not null;index:idx_carrier_messages_run,priority:1;index:idx_carrier_messages_workspace_run,priority:2"`
	Marketing         bool           `gorm:"not null;default:false"`
	RunID             string         `gorm:"type:varchar(64);not null;default:'';index:idx_carrier_messages_run,priority:2"`
	Variant           int            `gorm:"not null;default:0"`
	Content           datatypes.JSON `gorm:"type:jsonb;not null"`
	Text              string         `gorm:"type:text;not null;default:''"`
	Segments          int            `gorm:"not null;default:0"`
	Class             string         `gorm:"type:varchar(16);not null"`
	Step              string         `gorm:"type:varchar(16);not null"`
	Status            string         `gorm:"type:varchar(16);not null;index:idx_carrier_messages_overdue,priority:1"`
	ErrorCode         string         `gorm:"type:varchar(64);not null;default:''"`
	ParentMessageID   *string        `gorm:"type:uuid;index:idx_carrier_messages_parent"`
	FallbackMessageID *string        `gorm:"type:uuid"`
	Fallback          datatypes.JSON `gorm:"type:jsonb;not null"`
	ChargeReference   string         `gorm:"type:varchar(128);not null;default:''"`
	Operator          *string        `gorm:"type:varchar(64)"`
	AcceptedAt        *time.Time     `gorm:"type:timestamptz"`
	DeliveredAt       *time.Time     `gorm:"type:timestamptz"`
	ReadAt            *time.Time     `gorm:"type:timestamptz"`
	FailedAt          *time.Time     `gorm:"type:timestamptz"`
	ExpiresAt         *time.Time     `gorm:"type:timestamptz;index:idx_carrier_messages_overdue,priority:2"`
	CreatedAt         time.Time      `gorm:"type:timestamptz;not null;index:idx_carrier_messages_cap,priority:3;index:idx_carrier_messages_workspace_run,priority:3;index:idx_carrier_messages_sms_phone,priority:2"`
	UpdatedAt         time.Time      `gorm:"type:timestamptz;not null"`
}

func (CarrierMessage) TableName() string { return "carrier_messages" }

type CarrierMessageEvent struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null;index:idx_carrier_message_events_phone_kind,priority:1"`
	MessageID   *string   `gorm:"type:uuid;index:idx_carrier_message_events_message"`
	SenderID    *string   `gorm:"type:uuid"`
	Phone       string    `gorm:"type:varchar(20);not null;index:idx_carrier_message_events_phone_kind,priority:2"`
	Kind        string    `gorm:"type:varchar(16);not null;index:idx_carrier_message_events_phone_kind,priority:3"`
	ButtonCard  *int      `gorm:"column:button_card"`
	ButtonIndex *int      `gorm:"column:button_index"`
	Label       string    `gorm:"type:varchar(255);not null;default:''"`
	DedupKey    string    `gorm:"type:varchar(255);not null;uniqueIndex:ux_carrier_message_events_dedup"`
	OccurredAt  time.Time `gorm:"type:timestamptz;not null;index:idx_carrier_message_events_phone_kind,priority:4"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time `gorm:"type:timestamptz;not null"`
}

func (CarrierMessageEvent) TableName() string { return "carrier_message_events" }

type CarrierSuppression struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null;uniqueIndex:ux_carrier_suppressions_workspace_phone,priority:1"`
	Phone       string    `gorm:"type:varchar(20);not null;uniqueIndex:ux_carrier_suppressions_workspace_phone,priority:2"`
	Source      string    `gorm:"type:varchar(16);not null"`
	MessageID   *string   `gorm:"type:uuid"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time `gorm:"type:timestamptz;not null"`
}

func (CarrierSuppression) TableName() string { return "carrier_suppressions" }

type CarrierSettings struct {
	WorkspaceID     string         `gorm:"primaryKey;type:uuid"`
	QuietHours      datatypes.JSON `gorm:"type:jsonb;not null"`
	CapPerDay       int            `gorm:"column:frequency_cap_per_day;not null"`
	CapPerWeek      int            `gorm:"column:frequency_cap_per_week;not null"`
	AttributionDays int            `gorm:"not null"`
	CreatedAt       time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt       time.Time      `gorm:"type:timestamptz;not null"`
}

func (CarrierSettings) TableName() string { return "carrier_settings" }

func CarrierMessagingModels() []any {
	return []any{
		&RCSSender{},
		&SMSRoute{},
		&CarrierContact{},
		&RCSConversation{},
		&SMSConversation{},
		&CarrierMessage{},
		&CarrierMessageEvent{},
		&CarrierSuppression{},
		&CarrierSettings{},
	}
}
