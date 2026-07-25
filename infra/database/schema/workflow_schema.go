package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkflowSchema struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index"`
	DepartmentID  *string        `gorm:"type:uuid;index"`
	Name          string         `gorm:"not null;size:255"`
	Description   string         `gorm:"type:text"`
	Status        string         `gorm:"not null;size:20;default:draft;index"`
	Type          string         `gorm:"not null;size:20;default:messages;index"`
	TriggerType   string         `gorm:"not null;size:40"`
	TriggerConfig string         `gorm:"type:jsonb"`
	Graph         string         `gorm:"type:jsonb"`
	Version       int            `gorm:"not null;default:1"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

func (WorkflowSchema) TableName() string { return "workflows" }

func (w *WorkflowSchema) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

type WorkflowRunSchema struct {
	ID            string     `gorm:"primaryKey;type:uuid"`
	WorkflowID    string     `gorm:"type:uuid;not null;index"`
	WorkspaceID   string     `gorm:"type:uuid;not null;index"`
	EntryID       string     `gorm:"type:uuid;not null;index"`
	EntryType     string     `gorm:"not null;size:20"`
	Status        string     `gorm:"not null;size:20;default:running;index"`
	TriggerNodeID string     `gorm:"size:100;index"`
	CurrentNodeID string     `gorm:"size:100"`
	State         string     `gorm:"type:jsonb"`
	WakeAt        *int64     `gorm:"index"`
	WaitReason    string     `gorm:"size:30"`
	RetryCount    int        `gorm:"not null;default:0"`
	Error         string     `gorm:"type:text"`
	StartedAt     time.Time  `gorm:"autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime"`
	CompletedAt   *time.Time `gorm:"index"`
}

func (WorkflowRunSchema) TableName() string { return "workflow_runs" }

func (r *WorkflowRunSchema) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

type WorkflowRunLogSchema struct {
	ID         string    `gorm:"primaryKey;type:uuid"`
	RunID      string    `gorm:"type:uuid;not null;index"`
	NodeID     string    `gorm:"not null;size:100"`
	NodeType   string    `gorm:"not null;size:40"`
	Status     string    `gorm:"not null;size:20"`
	Input      string    `gorm:"type:jsonb"`
	Output     string    `gorm:"type:jsonb"`
	Error      string    `gorm:"type:text"`
	ExecutedAt time.Time `gorm:"autoCreateTime"`
}

func (WorkflowRunLogSchema) TableName() string { return "workflow_run_logs" }

func (l *WorkflowRunLogSchema) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}
