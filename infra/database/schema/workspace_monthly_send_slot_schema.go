package schema

import "time"

type WorkspaceMonthlySendSlot struct {
	ReferenceID string    `gorm:"primaryKey;type:varchar(255)"`
	WorkspaceID string    `gorm:"type:uuid;not null;index:idx_wmss_workspace_period,priority:1"`
	Period      time.Time `gorm:"not null;index:idx_wmss_workspace_period,priority:2"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (WorkspaceMonthlySendSlot) TableName() string {
	return "workspace_monthly_send_slots"
}
