package schema

import (
	"time"

	"gorm.io/datatypes"
)

type CampaignDraft struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_campaign_drafts_workspace_channel,priority:1"`
	Channel     string         `gorm:"type:varchar(32);not null;index:idx_campaign_drafts_workspace_channel,priority:2"`
	Name        string         `gorm:"type:varchar(255);not null;default:''"`
	Payload     datatypes.JSON `gorm:"type:jsonb;not null"`
	CreatedBy   string         `gorm:"type:uuid;not null"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null;index:idx_campaign_drafts_workspace_channel,priority:3,sort:desc"`
}

func (CampaignDraft) TableName() string {
	return "campaign_drafts"
}

func CampaignDraftModels() []any {
	return []any{&CampaignDraft{}}
}
