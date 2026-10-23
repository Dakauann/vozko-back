package schema

import (
	"time"

	"gorm.io/datatypes"
)

type CarrierCampaign struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_carrier_campaigns_workspace_created,priority:1"`
	Name        string         `gorm:"type:varchar(120);not null"`
	SenderID    string         `gorm:"type:uuid;not null;index:idx_carrier_campaigns_sender"`
	Channel     string         `gorm:"type:varchar(8);not null"`
	Status      string         `gorm:"type:varchar(16);not null;index:idx_carrier_campaigns_scheduled,priority:1,where:scheduled_at IS NOT NULL"`
	Kind        string         `gorm:"type:varchar(16);not null"`
	Variants    datatypes.JSON `gorm:"type:jsonb;not null"`
	Fallback    datatypes.JSON `gorm:"type:jsonb;not null"`
	Audience    datatypes.JSON `gorm:"type:jsonb;not null"`
	ScheduledAt *time.Time     `gorm:"type:timestamptz;index:idx_carrier_campaigns_scheduled,priority:2"`
	Recurrence  datatypes.JSON `gorm:"type:jsonb"`
	CreatedBy   OptionalText   `gorm:"type:uuid"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;index:idx_carrier_campaigns_workspace_created,priority:2"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null"`
}

func (CarrierCampaign) TableName() string { return "carrier_campaigns" }

type CarrierCampaignRun struct {
	ID          string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID string     `gorm:"type:uuid;not null"`
	CampaignID  string     `gorm:"type:uuid;not null;uniqueIndex:ux_carrier_campaign_runs_occurrence,priority:1"`
	Occurrence  time.Time  `gorm:"type:timestamptz;not null;uniqueIndex:ux_carrier_campaign_runs_occurrence,priority:2"`
	Status      string     `gorm:"type:varchar(16);not null;index:idx_carrier_campaign_runs_status"`
	StartedAt   time.Time  `gorm:"type:timestamptz;not null"`
	CompletedAt *time.Time `gorm:"type:timestamptz"`
	CreatedAt   time.Time  `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time  `gorm:"type:timestamptz;not null"`
}

func (CarrierCampaignRun) TableName() string { return "carrier_campaign_runs" }

type CarrierCampaignRecipient struct {
	ID          string       `gorm:"primaryKey;type:uuid;index:idx_carrier_campaign_recipients_pending,priority:3"`
	WorkspaceID string       `gorm:"type:uuid;not null"`
	CampaignID  string       `gorm:"type:uuid;not null;index:idx_carrier_campaign_recipients_campaign,priority:1"`
	RunID       string       `gorm:"type:uuid;not null;uniqueIndex:ux_carrier_campaign_recipients_lead,priority:1;index:idx_carrier_campaign_recipients_pending,priority:1"`
	LeadID      string       `gorm:"type:uuid;not null;uniqueIndex:ux_carrier_campaign_recipients_lead,priority:2"`
	Phone       string       `gorm:"type:varchar(20);not null;default:''"`
	Variant     int          `gorm:"not null;default:0;index:idx_carrier_campaign_recipients_campaign,priority:2"`
	Status      string       `gorm:"type:varchar(16);not null;index:idx_carrier_campaign_recipients_pending,priority:2"`
	SkipReason  string       `gorm:"type:varchar(24);not null;default:''"`
	MessageID   OptionalText `gorm:"type:uuid;uniqueIndex:ux_carrier_campaign_recipients_message,where:message_id IS NOT NULL"`
	ErrorCode   string       `gorm:"type:varchar(64);not null;default:''"`
	SentAt      *time.Time   `gorm:"type:timestamptz"`
	CreatedAt   time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time    `gorm:"type:timestamptz;not null"`
}

func (CarrierCampaignRecipient) TableName() string { return "carrier_campaign_recipients" }

func CarrierCampaignModels() []any {
	return []any{
		&CarrierCampaign{},
		&CarrierCampaignRun{},
		&CarrierCampaignRecipient{},
	}
}
