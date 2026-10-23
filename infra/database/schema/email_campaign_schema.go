package schema

import (
	"time"

	"gorm.io/datatypes"
)

type EmailCampaign struct {
	ID                 string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string         `gorm:"type:uuid;not null;index:idx_email_campaigns_workspace_created,priority:1"`
	Name               string         `gorm:"type:varchar(120);not null"`
	Status             string         `gorm:"type:varchar(16);not null;index:idx_email_campaigns_status"`
	HoldReason         string         `gorm:"type:varchar(40);not null;default:''"`
	SenderID           OptionalText   `gorm:"type:uuid"`
	Subject            string         `gorm:"type:varchar(150);not null;default:''"`
	Preheader          string         `gorm:"type:varchar(200);not null;default:''"`
	Document           datatypes.JSON `gorm:"type:jsonb"`
	Selection          datatypes.JSON `gorm:"type:jsonb"`
	ListID             OptionalText   `gorm:"type:uuid"`
	MergePolicies      datatypes.JSON `gorm:"type:jsonb"`
	ABTest             datatypes.JSON `gorm:"column:ab_test;type:jsonb"`
	UTM                datatypes.JSON `gorm:"column:utm;type:jsonb"`
	ScheduledAt        *time.Time     `gorm:"type:timestamptz;index:idx_email_campaigns_scheduled,where:scheduled_at IS NOT NULL"`
	StartedAt          *time.Time     `gorm:"type:timestamptz"`
	CompletedAt        *time.Time     `gorm:"type:timestamptz"`
	PaidUnits          int64          `gorm:"not null;default:0"`
	UsedUnits          int64          `gorm:"not null;default:0"`
	RefundedUnits      int64          `gorm:"not null;default:0"`
	RefundedMicros     int64          `gorm:"not null;default:0"`
	SettledAt          *time.Time     `gorm:"type:timestamptz"`
	TestBatchSize      int            `gorm:"not null;default:0"`
	TestBatchState     string         `gorm:"type:varchar(16);not null;default:'';index:idx_email_campaigns_test_batch_testing,where:test_batch_state = 'testing'"`
	TestBatchLastID    string         `gorm:"type:varchar(36);not null;default:''"`
	TestBatchDrainedAt *time.Time     `gorm:"type:timestamptz"`
	TestBatchDecidedAt *time.Time     `gorm:"type:timestamptz"`
	CreatedBy          OptionalText   `gorm:"type:uuid"`
	LaunchedBy         OptionalText   `gorm:"type:uuid"`
	CreatedAt          time.Time      `gorm:"type:timestamptz;not null;index:idx_email_campaigns_workspace_created,priority:2"`
	UpdatedAt          time.Time      `gorm:"type:timestamptz;not null"`
}

func (EmailCampaign) TableName() string {
	return "email_campaigns"
}

type EmailCampaignRecipient struct {
	ID            string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string       `gorm:"type:uuid;not null"`
	CampaignID    string       `gorm:"type:uuid;not null;index:idx_email_campaign_recipients_campaign_status,priority:1;uniqueIndex:ux_email_campaign_recipients_address,priority:1,where:skip_reason <> 'no_email' AND skip_reason <> 'invalid_address' AND skip_reason <> 'duplicate'"`
	LeadID        OptionalText `gorm:"type:uuid"`
	Address       string       `gorm:"type:varchar(254);not null;default:'';uniqueIndex:ux_email_campaign_recipients_address,priority:2"`
	Variant       string       `gorm:"type:varchar(8);not null;default:''"`
	Status        string       `gorm:"type:varchar(16);not null;index:idx_email_campaign_recipients_campaign_status,priority:2"`
	SkipReason    string       `gorm:"type:varchar(24);not null;default:''"`
	MessageID     OptionalText `gorm:"type:uuid;index:idx_email_campaign_recipients_message,where:message_id IS NOT NULL"`
	FailureCode   string       `gorm:"type:varchar(40);not null;default:''"`
	SentAt        *time.Time   `gorm:"type:timestamptz"`
	SlotClaimedAt *time.Time   `gorm:"type:timestamptz"`
	CreatedAt     time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt     time.Time    `gorm:"type:timestamptz;not null"`
}

func (EmailCampaignRecipient) TableName() string {
	return "email_campaign_recipients"
}

type EmailCampaignChargeBatch struct {
	CampaignID     string     `gorm:"primaryKey;type:uuid"`
	BatchNo        int        `gorm:"primaryKey;autoIncrement:false"`
	Units          int64      `gorm:"not null"`
	UnitMicros     int64      `gorm:"not null"`
	UnitCostMicros int64      `gorm:"not null"`
	TotalMicros    int64      `gorm:"not null"`
	RefundedUnits  int64      `gorm:"not null;default:0"`
	ChargedAt      *time.Time `gorm:"type:timestamptz"`
	RefundedAt     *time.Time `gorm:"type:timestamptz"`
	CreatedAt      time.Time  `gorm:"type:timestamptz;not null"`
}

func (EmailCampaignChargeBatch) TableName() string {
	return "email_campaign_charge_batches"
}

type EmailDesign struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string         `gorm:"type:uuid;not null;index:idx_email_designs_workspace_kind,priority:1"`
	Name         string         `gorm:"type:varchar(120);not null"`
	Document     datatypes.JSON `gorm:"type:jsonb;not null"`
	ThumbnailURL string         `gorm:"type:varchar(1024);not null;default:''"`
	Kind         string         `gorm:"type:varchar(16);not null;index:idx_email_designs_workspace_kind,priority:2"`
	CreatedBy    OptionalText   `gorm:"type:uuid"`
	CreatedAt    time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt    time.Time      `gorm:"type:timestamptz;not null"`
}

func (EmailDesign) TableName() string {
	return "email_designs"
}

type EmailSubscription struct {
	ID                string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID       string       `gorm:"type:uuid;not null;uniqueIndex:ux_email_subscriptions_address,priority:1"`
	Address           string       `gorm:"type:varchar(254);not null;uniqueIndex:ux_email_subscriptions_address,priority:2"`
	LeadID            OptionalText `gorm:"type:uuid"`
	Status            string       `gorm:"type:varchar(16);not null"`
	Source            string       `gorm:"type:varchar(16);not null;default:''"`
	ConsentedAt       *time.Time   `gorm:"type:timestamptz"`
	ConsentIP         string       `gorm:"type:varchar(45);not null;default:''"`
	UnsubscribedAt    *time.Time   `gorm:"type:timestamptz"`
	UnsubscribeSource string       `gorm:"type:varchar(16);not null;default:''"`
	CreatedAt         time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt         time.Time    `gorm:"type:timestamptz;not null"`
}

func (EmailSubscription) TableName() string {
	return "email_subscriptions"
}

func EmailCampaignModels() []any {
	return []any{
		&EmailCampaign{},
		&EmailCampaignRecipient{},
		&EmailCampaignChargeBatch{},
		&EmailDesign{},
		&EmailSubscription{},
	}
}
