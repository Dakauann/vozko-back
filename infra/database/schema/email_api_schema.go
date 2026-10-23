package schema

import (
	"time"

	"gorm.io/datatypes"
)

type APIKey struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_api_keys_workspace,priority:1"`
	Name        string         `gorm:"type:varchar(80);not null"`
	LookupID    string         `gorm:"type:varchar(16);not null;uniqueIndex:ux_api_keys_lookup"`
	SecretHash  string         `gorm:"type:char(64);not null"`
	Scopes      datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedBy   string         `gorm:"type:varchar(64);not null;default:''"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;index:idx_api_keys_workspace,priority:2"`
	LastUsedAt  *time.Time     `gorm:"type:timestamptz"`
	RevokedAt   *time.Time     `gorm:"type:timestamptz"`
	RevokedBy   string         `gorm:"type:varchar(64);not null;default:''"`
}

func (APIKey) TableName() string { return "api_keys" }

type EmailAPISend struct {
	ID                  string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID         string         `gorm:"type:uuid;not null;uniqueIndex:ux_email_api_sends_key,priority:1"`
	IdempotencyKey      string         `gorm:"type:varchar(256);not null;uniqueIndex:ux_email_api_sends_key,priority:2"`
	Fingerprint         string         `gorm:"type:char(64);not null"`
	Origin              string         `gorm:"type:varchar(16);not null"`
	APIKeyID            OptionalText   `gorm:"type:uuid"`
	SenderID            string         `gorm:"type:uuid;not null"`
	FromAddress         string         `gorm:"type:varchar(254);not null"`
	Subject             string         `gorm:"type:text;not null"`
	Tags                datatypes.JSON `gorm:"type:jsonb;not null"`
	Recipients          int            `gorm:"not null"`
	Round               int            `gorm:"not null;default:0"`
	RoundUnits          int64          `gorm:"not null;default:0"`
	RoundUnitMicros     int64          `gorm:"not null;default:0"`
	RoundUnitCostMicros int64          `gorm:"not null;default:0"`
	RoundSettled        bool           `gorm:"not null;default:true"`
	LeaseUntil          *time.Time     `gorm:"type:timestamptz"`
	CreatedAt           time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt           time.Time      `gorm:"type:timestamptz;not null"`
}

func (EmailAPISend) TableName() string { return "email_api_sends" }

type EmailAPIMessage struct {
	ID          string       `gorm:"primaryKey;type:uuid;index:idx_email_api_messages_log,priority:3,sort:desc;index:idx_email_api_messages_status,priority:4,sort:desc;index:idx_email_api_messages_address,priority:4,sort:desc"`
	SendID      string       `gorm:"type:uuid;not null;uniqueIndex:ux_email_api_messages_position,priority:1"`
	Position    int          `gorm:"not null;uniqueIndex:ux_email_api_messages_position,priority:2"`
	WorkspaceID string       `gorm:"type:uuid;not null;index:idx_email_api_messages_log,priority:1;index:idx_email_api_messages_status,priority:1;index:idx_email_api_messages_address,priority:1"`
	Role        string       `gorm:"type:varchar(8);not null"`
	Address     string       `gorm:"type:varchar(254);not null;index:idx_email_api_messages_address,priority:2"`
	Status      string       `gorm:"type:varchar(16);not null;index:idx_email_api_messages_status,priority:2"`
	ErrorCode   string       `gorm:"type:varchar(64);not null;default:''"`
	MessageID   OptionalText `gorm:"type:uuid;uniqueIndex:ux_email_api_messages_message"`
	Round       int          `gorm:"not null;default:0"`
	LastEvent   string       `gorm:"type:varchar(24);not null;default:''"`
	LastEventAt *time.Time   `gorm:"type:timestamptz"`
	CreatedAt   time.Time    `gorm:"type:timestamptz;not null;index:idx_email_api_messages_log,priority:2,sort:desc;index:idx_email_api_messages_status,priority:3,sort:desc;index:idx_email_api_messages_address,priority:3,sort:desc"`
	UpdatedAt   time.Time    `gorm:"type:timestamptz;not null"`
}

func (EmailAPIMessage) TableName() string { return "email_api_messages" }

type EmailAPIMessageTag struct {
	MessageRowID string    `gorm:"primaryKey;type:uuid;index:idx_email_api_message_tags_filter,priority:5,sort:desc"`
	Name         string    `gorm:"primaryKey;type:varchar(256);index:idx_email_api_message_tags_filter,priority:2"`
	WorkspaceID  string    `gorm:"type:uuid;not null;index:idx_email_api_message_tags_filter,priority:1"`
	Value        string    `gorm:"type:varchar(256);not null;index:idx_email_api_message_tags_filter,priority:3"`
	CreatedAt    time.Time `gorm:"type:timestamptz;not null;index:idx_email_api_message_tags_filter,priority:4,sort:desc"`
}

func (EmailAPIMessageTag) TableName() string { return "email_api_message_tags" }

type EmailWebhookEndpoint struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index:idx_email_webhook_endpoints_workspace,priority:1"`
	URL           string         `gorm:"type:varchar(2048);not null"`
	Events        datatypes.JSON `gorm:"type:jsonb;not null"`
	Enabled       bool           `gorm:"not null"`
	SecretCipher  []byte         `gorm:"type:bytea;not null"`
	SecretHint    string         `gorm:"type:varchar(8);not null"`
	FailingSince  *time.Time     `gorm:"type:timestamptz"`
	AlertedAt     *time.Time     `gorm:"type:timestamptz"`
	LastSuccessAt *time.Time     `gorm:"type:timestamptz"`
	LastFailureAt *time.Time     `gorm:"type:timestamptz"`
	CreatedBy     string         `gorm:"type:varchar(64);not null;default:''"`
	CreatedAt     time.Time      `gorm:"type:timestamptz;not null;index:idx_email_webhook_endpoints_workspace,priority:2"`
	UpdatedAt     time.Time      `gorm:"type:timestamptz;not null"`
}

func (EmailWebhookEndpoint) TableName() string { return "email_webhook_endpoints" }

type EmailWebhookDelivery struct {
	ID             string     `gorm:"primaryKey;type:uuid;index:idx_email_webhook_deliveries_endpoint,priority:3,sort:desc"`
	EndpointID     string     `gorm:"type:uuid;not null;uniqueIndex:ux_email_webhook_deliveries_event,priority:1;index:idx_email_webhook_deliveries_endpoint,priority:1"`
	WorkspaceID    string     `gorm:"type:uuid;not null"`
	EventID        string     `gorm:"type:varchar(40);not null;uniqueIndex:ux_email_webhook_deliveries_event,priority:2"`
	EventType      string     `gorm:"type:varchar(32);not null"`
	MessageID      string     `gorm:"type:uuid;not null"`
	Payload        string     `gorm:"type:text;not null"`
	Status         string     `gorm:"type:varchar(16);not null"`
	Attempts       int        `gorm:"not null;default:0"`
	NextAttemptAt  *time.Time `gorm:"type:timestamptz;index:idx_email_webhook_deliveries_due,where:status = 'retrying'"`
	LastStatusCode int        `gorm:"not null;default:0"`
	LastError      string     `gorm:"type:text;not null;default:''"`
	CreatedAt      time.Time  `gorm:"type:timestamptz;not null;index:idx_email_webhook_deliveries_endpoint,priority:2,sort:desc;index:idx_email_webhook_deliveries_created"`
	UpdatedAt      time.Time  `gorm:"type:timestamptz;not null;index:idx_email_webhook_deliveries_inflight,where:status = 'queued' OR status = 'sending'"`
	DeliveredAt    *time.Time `gorm:"type:timestamptz"`
}

func (EmailWebhookDelivery) TableName() string { return "email_webhook_deliveries" }

type EmailWebhookAttempt struct {
	DeliveryID    string     `gorm:"primaryKey;type:uuid"`
	Attempt       int        `gorm:"primaryKey"`
	StatusCode    int        `gorm:"not null;default:0"`
	DurationMS    int64      `gorm:"not null;default:0"`
	Error         string     `gorm:"type:text;not null;default:''"`
	NextAttemptAt *time.Time `gorm:"type:timestamptz"`
	AttemptedAt   time.Time  `gorm:"type:timestamptz;not null;index:idx_email_webhook_attempts_at"`
}

func (EmailWebhookAttempt) TableName() string { return "email_webhook_attempts" }

func EmailAPIModels() []any {
	return []any{
		&APIKey{},
		&EmailAPISend{},
		&EmailAPIMessage{},
		&EmailAPIMessageTag{},
		&EmailWebhookEndpoint{},
		&EmailWebhookDelivery{},
		&EmailWebhookAttempt{},
	}
}
