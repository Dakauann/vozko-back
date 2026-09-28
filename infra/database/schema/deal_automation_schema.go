package schema

import "time"

type DealAutomation struct {
	ID            string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string    `gorm:"type:uuid;not null;uniqueIndex:ux_deal_automation_channel,priority:1"`
	EntryType     string    `gorm:"type:varchar(30);not null;uniqueIndex:ux_deal_automation_channel,priority:2"`
	ContainerKind string    `gorm:"type:varchar(20);not null;default:'';uniqueIndex:ux_deal_automation_channel,priority:3"`
	ContainerID   string    `gorm:"type:varchar(64);not null;uniqueIndex:ux_deal_automation_channel,priority:4"`
	PipelineID    string    `gorm:"type:uuid;not null"`
	UpdatedBy     string    `gorm:"type:varchar(120);not null;default:''"`
	UpdatedAt     time.Time `gorm:"not null"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
}

func (DealAutomation) TableName() string { return "deal_automations" }
