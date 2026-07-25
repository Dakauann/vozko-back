package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AssignmentHistory struct {
	ID                string     `gorm:"primaryKey;type:text"`
	WorkspaceID       string     `gorm:"type:uuid;not null;index:idx_assign_hist_ws_actor,priority:1;index:idx_assign_hist_entry,priority:1"`
	EntryID           string     `gorm:"type:uuid;not null;index:idx_assign_hist_entry,priority:2"`
	EntryType         string     `gorm:"type:varchar(20);not null;index:idx_assign_hist_entry,priority:3"`
	ActorKind         string     `gorm:"type:varchar(16);not null;default:'human'"`
	AssignedActorID   string     `gorm:"type:text;not null;index:idx_assign_hist_ws_actor,priority:2"`
	PreviousActorID   string     `gorm:"type:text;default:''"`
	Trigger           string     `gorm:"type:varchar(32);not null;default:''"`
	AssignedByActorID string     `gorm:"type:text;default:''"`
	BusinessPhoneID   *string    `gorm:"type:uuid;default:null"`
	SIPTrunkID        string     `gorm:"type:text;default:''"`
	DepartmentID      string     `gorm:"type:text;default:''"`
	StartedAt         time.Time  `gorm:"not null;index:idx_assign_hist_ws_actor,priority:3"`
	EndedAt           *time.Time `gorm:"index:idx_assign_hist_open,where:ended_at IS NULL"`
	CreatedAt         time.Time  `gorm:"autoCreateTime"`
}

func (AssignmentHistory) TableName() string {
	return "assignment_history"
}

func (h *AssignmentHistory) BeforeCreate(tx *gorm.DB) error {
	if h.ID == "" {
		h.ID = uuid.New().String()
	}
	return nil
}
