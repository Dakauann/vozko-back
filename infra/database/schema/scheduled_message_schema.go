package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScheduledMessage is one operator message parked for later delivery.
//
// Status carries a plain index rather than a composite with scheduled_at: the
// query that matters (everything due) is served by a PARTIAL index on
// scheduled_at restricted to pending rows, declared in indexes.go because GORM
// tags cannot express a WHERE clause. That is what keeps the sweep's cost
// proportional to the messages actually due rather than to the table.
type ScheduledMessage struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_sched_msg_ws"`
	EntryID     string `gorm:"type:uuid;not null;index:idx_sched_msg_entry,priority:1"`
	EntryType   string `gorm:"size:32;not null;index:idx_sched_msg_entry,priority:2"`

	CreatedByUserID  string  `gorm:"type:uuid;not null;index"`
	Text             string  `gorm:"type:text"`
	MediaID          *string `gorm:"type:uuid"`
	MediaType        *string `gorm:"size:20"`
	ReplyToMessageID *string `gorm:"type:uuid"`
	Signed           bool    `gorm:"not null;default:false"`

	ScheduledAt               time.Time `gorm:"not null;index"`
	WindowExpiresAtAtCreation *time.Time

	Status        string  `gorm:"size:20;not null;default:'pending';index"`
	FailureReason *string `gorm:"size:40"`
	FailureDetail string  `gorm:"type:text"`

	ClaimedAt     *time.Time
	SentAt        *time.Time
	SentMessageID *string `gorm:"type:uuid"`

	// IdempotencyKey is unique per workspace, enforced by a PARTIAL unique index
	// (indexes.go) so the many rows without one do not collide on NULL.
	IdempotencyKey *string `gorm:"size:128"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (ScheduledMessage) TableName() string {
	return "scheduled_messages"
}

func (m *ScheduledMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
