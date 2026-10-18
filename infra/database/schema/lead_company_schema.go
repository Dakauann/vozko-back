package schema

import "time"

type LeadCompany struct {
	ID           string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string       `gorm:"type:uuid;not null;index:idx_lead_companies_workspace,where:deleted_at IS NULL"`
	Name         string       `gorm:"type:varchar(120);not null"`
	AcceptsLeads bool         `gorm:"not null;default:true"`
	ImageMediaID OptionalText `gorm:"type:uuid"`
	CreatedBy    OptionalText `gorm:"type:uuid"`
	CreatedAt    time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt    time.Time    `gorm:"type:timestamptz;not null"`
	DeletedAt    *time.Time   `gorm:"type:timestamptz"`
}

func (LeadCompany) TableName() string {
	return "lead_companies"
}
