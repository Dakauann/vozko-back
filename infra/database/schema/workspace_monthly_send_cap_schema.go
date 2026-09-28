package schema

import "time"

type WorkspaceMonthlySendCap struct {
	WorkspaceID  string    `gorm:"primaryKey;type:uuid"`
	MonthlyLimit int64     `gorm:"not null;check:chk_wmsc_monthly_limit_positive,monthly_limit > 0"`
	UpdatedBy    string    `gorm:"type:uuid;not null"`
	UpdatedAt    time.Time `gorm:"not null"`
	UnlockedBy   *string   `gorm:"type:uuid"`
	UnlockedAt   *time.Time
	CreatedAt    time.Time `gorm:"autoCreateTime"`

	Workspace Workspace `gorm:"foreignKey:WorkspaceID;references:ID;constraint:OnDelete:CASCADE"`
}

func (WorkspaceMonthlySendCap) TableName() string {
	return "workspace_monthly_send_caps"
}
