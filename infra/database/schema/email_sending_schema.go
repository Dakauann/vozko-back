package schema

import (
	"time"

	"gorm.io/datatypes"
)

type EmailSenderAccount struct {
	ID                 string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string     `gorm:"type:uuid;not null;uniqueIndex:ux_email_sender_accounts_workspace"`
	Provider           string     `gorm:"type:varchar(16);not null"`
	TenantName         string     `gorm:"type:varchar(64);not null;uniqueIndex:ux_email_sender_accounts_tenant"`
	TenantRef          string     `gorm:"type:varchar(255);not null;default:''"`
	Mode               string     `gorm:"type:varchar(16);not null;index:idx_email_sender_accounts_mode"`
	Status             string     `gorm:"type:varchar(24);not null"`
	StatusReason       string     `gorm:"type:text;not null;default:''"`
	Slug               string     `gorm:"type:varchar(30);not null;uniqueIndex:ux_email_sender_accounts_slug"`
	ProbationStartedAt *time.Time `gorm:"type:timestamptz"`
	PromotedAt         *time.Time `gorm:"type:timestamptz"`
	UnusedWarnedAt     *time.Time `gorm:"type:timestamptz"`
	CreatedAt          time.Time  `gorm:"type:timestamptz;not null"`
	UpdatedAt          time.Time  `gorm:"type:timestamptz;not null"`
}

func (EmailSenderAccount) TableName() string { return "email_sender_accounts" }

type EmailSendingDomain struct {
	ID              string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string         `gorm:"type:uuid;not null;index:idx_email_sending_domains_workspace"`
	AccountID       string         `gorm:"type:uuid;not null"`
	Domain          string         `gorm:"type:varchar(253);not null;uniqueIndex:ux_email_sending_domains_domain"`
	Kind            string         `gorm:"type:varchar(24);not null"`
	Status          string         `gorm:"type:varchar(16);not null;index:idx_email_sending_domains_status"`
	ConnectMethod   string         `gorm:"type:varchar(16);not null"`
	MailFromDomain  string         `gorm:"type:varchar(253);not null"`
	ReplyDomain     string         `gorm:"type:varchar(253);not null;uniqueIndex:ux_email_sending_domains_reply"`
	Records         datatypes.JSON `gorm:"type:jsonb"`
	FailureReason   string         `gorm:"type:text;not null;default:''"`
	SetupFailure    string         `gorm:"type:varchar(32);not null;default:''"`
	VerifiedAt      *time.Time     `gorm:"type:timestamptz"`
	LastCheckedAt   *time.Time     `gorm:"type:timestamptz"`
	VerifyingSince  *time.Time     `gorm:"type:timestamptz"`
	RetiredAt       *time.Time     `gorm:"type:timestamptz"`
	ReleaseState    string         `gorm:"type:varchar(16);not null;default:''"`
	RepliesPausedAt *time.Time     `gorm:"type:timestamptz;index:idx_email_sending_domains_replies_paused,where:replies_paused_at IS NOT NULL"`
	CreatedAt       time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt       time.Time      `gorm:"type:timestamptz;not null"`
}

func (EmailSendingDomain) TableName() string { return "email_sending_domains" }

type EmailSender struct {
	ID             string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string    `gorm:"type:uuid;not null;index:idx_email_senders_workspace;uniqueIndex:ux_email_senders_default,where:is_default"`
	DomainID       string    `gorm:"type:uuid;not null;uniqueIndex:ux_email_senders_local_part,priority:1"`
	LocalPart      string    `gorm:"type:varchar(64);not null;uniqueIndex:ux_email_senders_local_part,priority:2"`
	DisplayName    string    `gorm:"type:varchar(80);not null"`
	ReplyForwardTo string    `gorm:"type:varchar(254);not null;default:''"`
	IsDefault      bool      `gorm:"not null;default:false"`
	CreatedAt      time.Time `gorm:"type:timestamptz;not null"`
	UpdatedAt      time.Time `gorm:"type:timestamptz;not null"`
}

func (EmailSender) TableName() string { return "email_senders" }

type EmailDailyUsage struct {
	DomainID  string    `gorm:"primaryKey;type:uuid"`
	Day       string    `gorm:"primaryKey;type:varchar(10)"`
	Used      int64     `gorm:"not null;default:0"`
	Cap       int64     `gorm:"not null;default:0"`
	UpdatedAt time.Time `gorm:"type:timestamptz;not null"`
}

func (EmailDailyUsage) TableName() string { return "email_daily_usage" }

type EmailSuppression struct {
	ID              string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string    `gorm:"type:uuid;not null;uniqueIndex:ux_email_suppressions_address,priority:1"`
	Address         string    `gorm:"type:varchar(254);not null;uniqueIndex:ux_email_suppressions_address,priority:2"`
	Reason          string    `gorm:"type:varchar(16);not null"`
	SourceMessageID string    `gorm:"type:varchar(64);not null;default:''"`
	SuppressedAt    time.Time `gorm:"type:timestamptz;not null"`
	CreatedAt       time.Time `gorm:"type:timestamptz;not null"`
	UpdatedAt       time.Time `gorm:"type:timestamptz;not null"`
}

func (EmailSuppression) TableName() string { return "email_suppressions" }

type EmailMessage struct {
	ID                string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string       `gorm:"type:uuid;not null;index:idx_email_messages_run,priority:1;index:idx_email_messages_workspace_sent,priority:1"`
	ProviderMessageID OptionalText `gorm:"type:varchar(255);uniqueIndex:ux_email_messages_provider"`
	RunKind           string       `gorm:"type:varchar(16);not null;index:idx_email_messages_run,priority:2"`
	RunID             string       `gorm:"type:varchar(64);not null;default:'';index:idx_email_messages_run,priority:3"`
	RecipientID       string       `gorm:"type:varchar(64);not null;default:''"`
	DomainID          string       `gorm:"type:uuid;not null"`
	Address           string       `gorm:"type:varchar(254);not null;index:idx_email_messages_address"`
	Status            string       `gorm:"type:varchar(16);not null"`
	SentAt            *time.Time   `gorm:"type:timestamptz;index:idx_email_messages_workspace_sent,priority:2"`
	DeliveredAt       *time.Time   `gorm:"type:timestamptz"`
	FirstOpenAt       *time.Time   `gorm:"type:timestamptz"`
	FirstClickAt      *time.Time   `gorm:"type:timestamptz"`
	BouncedAt         *time.Time   `gorm:"type:timestamptz"`
	BounceKind        string       `gorm:"type:varchar(16);not null;default:''"`
	ComplainedAt      *time.Time   `gorm:"type:timestamptz"`
	UnsubscribedAt    *time.Time   `gorm:"type:timestamptz"`
	Opens             int          `gorm:"not null;default:0"`
	BotOpens          int          `gorm:"not null;default:0"`
	Clicks            int          `gorm:"not null;default:0"`
	CreatedAt         time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt         time.Time    `gorm:"type:timestamptz;not null"`
}

func (EmailMessage) TableName() string { return "email_messages" }

type EmailMessageEvent struct {
	ID                string         `gorm:"primaryKey;type:uuid"`
	MessageID         string         `gorm:"type:uuid;not null;uniqueIndex:ux_email_message_events_dedup,priority:1"`
	WorkspaceID       string         `gorm:"type:uuid;not null"`
	ProviderMessageID string         `gorm:"type:varchar(255);not null"`
	Kind              string         `gorm:"type:varchar(24);not null;uniqueIndex:ux_email_message_events_dedup,priority:2"`
	OccurredAt        time.Time      `gorm:"type:timestamptz;not null;uniqueIndex:ux_email_message_events_dedup,priority:3"`
	LinkHash          string         `gorm:"type:varchar(64);not null;default:'';uniqueIndex:ux_email_message_events_dedup,priority:4"`
	Link              string         `gorm:"type:text;not null;default:''"`
	UserAgent         string         `gorm:"type:text;not null;default:''"`
	IsBot             bool           `gorm:"not null;default:false"`
	Detail            datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt         time.Time      `gorm:"type:timestamptz;not null"`
}

func (EmailMessageEvent) TableName() string { return "email_message_events" }

func EmailSendingModels() []any {
	return append([]any{
		&EmailSenderAccount{},
		&EmailSendingDomain{},
		&EmailSender{},
		&EmailDailyUsage{},
		&EmailSuppression{},
		&EmailMessage{},
		&EmailMessageEvent{},
	}, EmailHygieneModels()...)
}
