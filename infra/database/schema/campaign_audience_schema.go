package schema

import (
	"time"

	"gorm.io/datatypes"
)

type CampaignAudienceList struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_campaign_audience_lists_workspace,priority:1"`
	CreatedBy   string         `gorm:"type:uuid;not null;index:idx_campaign_audience_lists_workspace,priority:2"`
	Channel     string         `gorm:"type:varchar(16);not null"`
	FileName    string         `gorm:"type:varchar(255);not null;default:''"`
	Layout      datatypes.JSON `gorm:"type:jsonb;not null"`
	Lines       int            `gorm:"not null;default:0"`
	IssueCounts datatypes.JSON `gorm:"type:jsonb"`
	Issues      datatypes.JSON `gorm:"type:jsonb"`
	AttachedTo  OptionalText   `gorm:"type:varchar(80);index:idx_campaign_audience_lists_attached,where:attached_to IS NOT NULL"`
	ExpiresAt   *time.Time     `gorm:"type:timestamptz;index:idx_campaign_audience_lists_expires,where:attached_to IS NULL"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null"`
}

func (CampaignAudienceList) TableName() string {
	return "campaign_audience_lists"
}

type CampaignAudienceListRow struct {
	ID        string         `gorm:"primaryKey;type:uuid"`
	ListID    string         `gorm:"type:uuid;not null;uniqueIndex:ux_campaign_audience_list_rows_contact,priority:1;index:idx_campaign_audience_list_rows_order,priority:1"`
	Line      int            `gorm:"not null;default:0;index:idx_campaign_audience_list_rows_order,priority:2"`
	Contact   string         `gorm:"type:varchar(254);not null;uniqueIndex:ux_campaign_audience_list_rows_contact,priority:2"`
	Name      string         `gorm:"type:varchar(255);not null;default:''"`
	Cells     datatypes.JSON `gorm:"type:jsonb"`
	Variables datatypes.JSON `gorm:"type:jsonb"`
	Metadata  datatypes.JSON `gorm:"type:jsonb"`
}

func (CampaignAudienceListRow) TableName() string {
	return "campaign_audience_list_rows"
}

type WhatsAppTemplateVariableSuggestion struct {
	WorkspaceID string    `gorm:"primaryKey;type:uuid"`
	TemplateID  string    `gorm:"primaryKey;type:uuid"`
	Slot        int       `gorm:"primaryKey"`
	Source      string    `gorm:"type:varchar(160);not null"`
	UpdatedBy   string    `gorm:"type:uuid;not null"`
	UpdatedAt   time.Time `gorm:"type:timestamptz;not null"`
}

func (WhatsAppTemplateVariableSuggestion) TableName() string {
	return "whatsapp_template_variable_suggestions"
}

func CampaignAudienceModels() []any {
	return []any{&CampaignAudienceList{}, &CampaignAudienceListRow{}, &WhatsAppTemplateVariableSuggestion{}}
}
