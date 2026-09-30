package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CallQueue struct {
	ID             string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string         `gorm:"type:uuid;not null;index:idx_call_queue_ws_del,priority:1"`
	Name           string         `gorm:"size:80;not null"`
	Strategy       string         `gorm:"size:24;not null"`
	DepartmentID   string         `gorm:"size:64"`
	MemberUserIDs  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"`
	RingSeconds    int            `gorm:"not null"`
	MaxWaitSeconds int            `gorm:"not null"`
	WrapUpSeconds  int            `gorm:"not null"`
	HoldPresetID   string         `gorm:"size:64"`
	HoldMediaID    string         `gorm:"size:64"`
	CreatedAt      time.Time      `gorm:"autoCreateTime"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime"`
	DeletedAt      gorm.DeletedAt `gorm:"index;index:idx_call_queue_ws_del,priority:2"`
}

func (CallQueue) TableName() string { return "call_queues" }

func (q *CallQueue) BeforeCreate(tx *gorm.DB) error {
	if q.ID == "" {
		q.ID = uuid.New().String()
	}
	return nil
}

type CallTransfer struct {
	ID            string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string     `gorm:"type:uuid;not null;index:idx_call_transfer_ws_created,priority:1"`
	CallID        string     `gorm:"size:160;not null;index"`
	FromUserID    string     `gorm:"size:64;not null"`
	TargetKind    string     `gorm:"size:16;not null"`
	TargetQueueID string     `gorm:"size:64"`
	TargetUserID  string     `gorm:"size:64"`
	Notes         string     `gorm:"type:text"`
	Outcome       string     `gorm:"size:16;not null"`
	AnsweredBy    string     `gorm:"size:64"`
	CreatedAt     time.Time  `gorm:"not null;index:idx_call_transfer_ws_created,priority:2"`
	FinishedAt    *time.Time
}

func (CallTransfer) TableName() string { return "call_transfers" }

type CallRoutingSettings struct {
	WorkspaceID  string    `gorm:"primaryKey;type:uuid"`
	HoldPresetID string    `gorm:"size:64"`
	HoldMediaID  string    `gorm:"size:64"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

func (CallRoutingSettings) TableName() string { return "call_routing_settings" }
