package schema

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Notification struct {
	ID             string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string         `gorm:"type:uuid;not null;uniqueIndex:idx_notifications_dedup,priority:1,where:dedup_key IS NOT NULL"`
	Category       string         `gorm:"type:varchar(32);not null"`
	Severity       string         `gorm:"type:varchar(16);not null"`
	Title          string         `gorm:"type:varchar(160);not null"`
	Body           string         `gorm:"type:text;not null;default:''"`
	ImageURL       string         `gorm:"type:text;not null;default:''"`
	ActionURL      string         `gorm:"type:text;not null;default:''"`
	ActionLabel    string         `gorm:"type:varchar(80);not null;default:''"`
	Audience       datatypes.JSON `gorm:"type:jsonb;not null"`
	Email          bool           `gorm:"not null;default:false"`
	EmailCopy      datatypes.JSON `gorm:"type:jsonb"`
	DedupKey       *string        `gorm:"type:varchar(255);uniqueIndex:idx_notifications_dedup,priority:2,where:dedup_key IS NOT NULL"`
	Source         string         `gorm:"type:varchar(64);not null;default:''"`
	BroadcastID    *string        `gorm:"type:uuid;index:idx_notifications_broadcast"`
	CreatedBy      *string        `gorm:"type:uuid"`
	Status         string         `gorm:"type:varchar(16);not null;index:idx_notifications_pending,priority:1,where:status = 'pending'"`
	RecipientCount int            `gorm:"not null;default:0"`
	CreatedAt      time.Time      `gorm:"type:timestamptz;not null;index:idx_notifications_created;index:idx_notifications_pending,priority:2,where:status = 'pending'"`
	DeliveredAt    *time.Time     `gorm:"type:timestamptz"`
}

func (Notification) TableName() string { return "notifications" }

func (n *Notification) BeforeCreate(*gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return nil
}

type NotificationRecipient struct {
	NotificationID string     `gorm:"primaryKey;type:uuid;index:idx_notification_inbox,priority:4,sort:desc;index:idx_notification_inbox_category,priority:5,sort:desc"`
	UserID         string     `gorm:"primaryKey;type:uuid;index:idx_notification_inbox,priority:2;index:idx_notification_inbox_category,priority:2;index:idx_notification_unread,priority:2,where:read_at IS NULL"`
	WorkspaceID    string     `gorm:"type:uuid;not null;index:idx_notification_inbox,priority:1;index:idx_notification_inbox_category,priority:1;index:idx_notification_unread,priority:1,where:read_at IS NULL;index:idx_notification_digest_held,priority:1,where:digest_pending"`
	Category       string     `gorm:"type:varchar(32);not null;index:idx_notification_inbox_category,priority:3;index:idx_notification_unread,priority:3,where:read_at IS NULL"`
	CreatedAt      time.Time  `gorm:"type:timestamptz;not null;index:idx_notification_inbox,priority:3,sort:desc;index:idx_notification_inbox_category,priority:4,sort:desc;index:idx_notification_digest_held,priority:2,where:digest_pending"`
	ReadAt         *time.Time `gorm:"type:timestamptz"`
	DigestPending  bool       `gorm:"not null;default:false"`
}

func (NotificationRecipient) TableName() string { return "notification_recipients" }

type NotificationPreference struct {
	WorkspaceID string         `gorm:"primaryKey;type:uuid"`
	UserID      string         `gorm:"primaryKey;type:uuid"`
	EmailMuted  pq.StringArray `gorm:"type:text[];not null;default:'{}'"`
	EmailDigest bool           `gorm:"not null;default:false"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null"`
}

func (NotificationPreference) TableName() string { return "notification_preferences" }

type NotificationBroadcast struct {
	ID             string         `gorm:"primaryKey;type:uuid;index:idx_notification_broadcasts_recent,priority:2,sort:desc"`
	Target         datatypes.JSON `gorm:"type:jsonb;not null"`
	Audience       datatypes.JSON `gorm:"type:jsonb;not null"`
	Category       string         `gorm:"type:varchar(32);not null"`
	Severity       string         `gorm:"type:varchar(16);not null"`
	Title          string         `gorm:"type:varchar(160);not null"`
	Body           string         `gorm:"type:text;not null;default:''"`
	ImageURL       string         `gorm:"type:text;not null;default:''"`
	ActionURL      string         `gorm:"type:text;not null;default:''"`
	ActionLabel    string         `gorm:"type:varchar(80);not null;default:''"`
	Email          bool           `gorm:"not null;default:false"`
	CreatedBy      string         `gorm:"type:uuid;not null"`
	Status         string         `gorm:"type:varchar(16);not null;index:idx_notification_broadcasts_queued,priority:1,where:status = 'queued'"`
	WorkspaceCount int            `gorm:"not null;default:0"`
	CreatedAt      time.Time      `gorm:"type:timestamptz;not null;index:idx_notification_broadcasts_recent,priority:1,sort:desc;index:idx_notification_broadcasts_queued,priority:2,where:status = 'queued'"`
	CompletedAt    *time.Time     `gorm:"type:timestamptz"`
}

func (NotificationBroadcast) TableName() string { return "notification_broadcasts" }

func (b *NotificationBroadcast) BeforeCreate(*gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

type NotificationEpisode struct {
	ID             string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string     `gorm:"type:uuid;not null;uniqueIndex:idx_notification_episodes_open,priority:1,where:closed_at IS NULL"`
	ConditionKey   string     `gorm:"type:varchar(255);not null;uniqueIndex:idx_notification_episodes_open,priority:2,where:closed_at IS NULL"`
	StepsSent      int        `gorm:"type:smallint;not null;default:0"`
	OpenedAt       time.Time  `gorm:"type:timestamptz;not null"`
	LastSeenAt     time.Time  `gorm:"type:timestamptz;not null;index:idx_notification_episodes_last_seen"`
	LastSentAt     *time.Time `gorm:"type:timestamptz"`
	NotificationID *string    `gorm:"type:uuid"`
	ClosedAt       *time.Time `gorm:"type:timestamptz"`
	CloseReason    string     `gorm:"type:varchar(16);not null;default:''"`
}

func (NotificationEpisode) TableName() string { return "notification_episodes" }

func (e *NotificationEpisode) BeforeCreate(*gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}
