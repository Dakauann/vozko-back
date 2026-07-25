package schema

import "time"

type CallRouletteState struct {
	ID                 string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string    `gorm:"type:uuid;not null;uniqueIndex:idx_call_rr_ws_trunk_dept,priority:1"`
	SIPTrunkID         string    `gorm:"type:uuid;not null;uniqueIndex:idx_call_rr_ws_trunk_dept,priority:2"`
	DepartmentID       string    `gorm:"type:text;not null;default:'';uniqueIndex:idx_call_rr_ws_trunk_dept,priority:3"`
	LastAssignedUserID string    `gorm:"type:text;not null;default:''"`
	CreatedAt          time.Time `gorm:"autoCreateTime"`
	UpdatedAt          time.Time `gorm:"autoUpdateTime"`
}

func (CallRouletteState) TableName() string {
	return "call_roulette_state"
}

type CallRouletteAssignment struct {
	ID             string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID    string    `gorm:"type:uuid;not null;index:idx_call_assign_ws"`
	SIPTrunkID     string    `gorm:"type:uuid;not null;index"`
	EntryID        string    `gorm:"type:uuid;not null;uniqueIndex:idx_call_assign_entry"`
	EntryType      string    `gorm:"type:varchar(20);not null;default:'voice'"`
	AssignedUserID string    `gorm:"type:uuid;not null;index:idx_call_assign_user;index:idx_call_assign_ws_user,priority:2"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

func (CallRouletteAssignment) TableName() string {
	return "call_roulette_assignments"
}
