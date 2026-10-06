package schema

import "time"

type WorkspaceMonthlySendCap struct {
	WorkspaceID  string    `gorm:"primaryKey;type:uuid"`
	MonthlyLimit int64     `gorm:"not null;check:chk_wmsc_monthly_limit_positive,monthly_limit > 0"`
	CycleDay     int       `gorm:"not null;default:1;check:chk_wmsc_cycle_day,cycle_day BETWEEN 1 AND 31"`
	UpdatedBy    string    `gorm:"type:uuid;not null"`
	UpdatedAt    time.Time `gorm:"not null"`
	UnlockedBy   *string   `gorm:"type:uuid"`
	UnlockedAt   *time.Time
	CountedFrom  *time.Time
	Used         int64     `gorm:"not null;default:0"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`

	Workspace Workspace `gorm:"foreignKey:WorkspaceID;references:ID;constraint:OnDelete:CASCADE"`
}

func (WorkspaceMonthlySendCap) TableName() string {
	return "workspace_monthly_send_caps"
}
