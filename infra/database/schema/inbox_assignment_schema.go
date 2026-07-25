package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InboxAssignment struct {
	ID          string `gorm:"primaryKey;type:text"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_inbox_assign_workspace;index:idx_inbox_assign_ws_user_type,priority:1"`
	// Nullable: a manual owner-assign (and any voice entry) has no business phone,
	// so this must be NULL, an empty string is not a valid uuid (SQLSTATE 22P02).
	BusinessPhoneID *string   `gorm:"type:uuid;default:null;index:idx_inbox_assign_phone"`
	EntryID         string    `gorm:"type:uuid;not null;uniqueIndex:idx_inbox_assign_entry;index:idx_inbox_assign_user_entry,priority:2"`
	EntryType       string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_inbox_assign_entry;index:idx_inbox_assign_ws_user_type,priority:3"`
	AssignedUserID  string    `gorm:"type:uuid;not null;index:idx_inbox_assign_user;index:idx_inbox_assign_user_entry,priority:1;index:idx_inbox_assign_ws_user_type,priority:2"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime"`
}

func (InboxAssignment) TableName() string {
	return "inbox_assignments"
}

func (a *InboxAssignment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type InboxRoundRobinState struct {
	ID                 string    `gorm:"primaryKey;type:text"`
	WorkspaceID        string    `gorm:"type:uuid;not null;uniqueIndex:idx_rr_state_ws_phone_dept"`
	BusinessPhoneID    string    `gorm:"type:uuid;not null;uniqueIndex:idx_rr_state_ws_phone_dept"`
	DepartmentID       string    `gorm:"type:text;not null;default:'';uniqueIndex:idx_rr_state_ws_phone_dept"`
	LastAssignedUserID string    `gorm:"type:text;not null;default:''"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime"`
}

func (InboxRoundRobinState) TableName() string {
	return "inbox_round_robin_state"
}

func (s *InboxRoundRobinState) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
