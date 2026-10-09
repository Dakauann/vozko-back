package schema

import (
	"time"

	"gorm.io/datatypes"
)

type LeadActionRun struct {
	ID                 string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string         `gorm:"type:uuid;not null;uniqueIndex:ux_lead_action_runs_key,priority:1;index:idx_lead_action_runs_workspace_created,priority:1"`
	ActorID            string         `gorm:"type:uuid;not null"`
	IsAdmin            bool           `gorm:"not null;default:false"`
	DepartmentID       OptionalText   `gorm:"type:uuid"`
	Action             string         `gorm:"type:varchar(32);not null"`
	Params             datatypes.JSON `gorm:"type:jsonb;not null"`
	IdempotencyKey     string         `gorm:"type:varchar(128);not null;uniqueIndex:ux_lead_action_runs_key,priority:2"`
	RequestFingerprint string         `gorm:"type:varchar(64);not null;default:''"`
	Status             string         `gorm:"type:varchar(16);not null;index:idx_lead_action_runs_status_heartbeat,priority:1"`
	Phase              string         `gorm:"type:varchar(16);not null;default:'edit'"`
	Cursor             OptionalText   `gorm:"type:uuid"`
	Result             datatypes.JSON `gorm:"type:jsonb;not null"`
	FailureCode        OptionalText   `gorm:"type:varchar(32)"`
	Attempts           int            `gorm:"not null;default:0"`
	ClaimToken         OptionalText   `gorm:"type:varchar(64)"`
	HeartbeatAt        *time.Time     `gorm:"type:timestamptz;index:idx_lead_action_runs_status_heartbeat,priority:2"`
	NotBefore          *time.Time     `gorm:"type:timestamptz"`
	StartedAt          *time.Time     `gorm:"type:timestamptz"`
	FinishedAt         *time.Time     `gorm:"type:timestamptz"`
	CreatedAt          time.Time      `gorm:"type:timestamptz;not null;index:idx_lead_action_runs_workspace_created,priority:2"`
	UpdatedAt          time.Time      `gorm:"type:timestamptz;not null"`
}

func (LeadActionRun) TableName() string {
	return "lead_action_runs"
}

type LeadSelectionSnapshot struct {
	SnapshotID  string    `gorm:"primaryKey;type:uuid"`
	LeadID      string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null;index:idx_lead_selection_snapshots_created"`
}

func (LeadSelectionSnapshot) TableName() string {
	return "lead_selection_snapshots"
}
