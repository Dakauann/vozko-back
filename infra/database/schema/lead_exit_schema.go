package schema

import "time"

type LeadExitRun struct {
	WorkspaceID        string    `gorm:"type:uuid;primaryKey"`
	InboundMessageID   string    `gorm:"type:varchar(255);primaryKey"`
	EntryID            string    `gorm:"type:varchar(64);not null;default:''"`
	EntryType          string    `gorm:"type:varchar(32);not null;default:''"`
	LeadID             string    `gorm:"type:varchar(64);not null;default:''"`
	TemplateMessageID  string    `gorm:"type:varchar(255);not null;default:''"`
	ExitTrigger        string    `gorm:"type:varchar(32);not null"`
	Kind               string    `gorm:"type:varchar(32);not null"`
	Outcome            string    `gorm:"type:varchar(16);not null"`
	Reason             string    `gorm:"type:varchar(64);not null;default:''"`
	AssignedUserID     string    `gorm:"type:varchar(64);not null;default:''"`
	ConsentEventID     string    `gorm:"type:varchar(64);not null;default:''"`
	Confirmation       string    `gorm:"type:varchar(16);not null;default:''"`
	ConfirmationReason string    `gorm:"type:varchar(64);not null;default:''"`
	ClaimedAt          time.Time `gorm:"type:timestamptz;not null"`
	UpdatedAt          time.Time `gorm:"type:timestamptz;not null"`
}

func (LeadExitRun) TableName() string { return "lead_exit_runs" }
