package schema

import (
	"time"

	"gorm.io/datatypes"
)

type LeadArea struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_lead_areas_workspace"`
	OwnerID     string         `gorm:"type:uuid;not null"`
	Visibility  string         `gorm:"type:varchar(16);not null"`
	Name        string         `gorm:"type:varchar(80);not null"`
	Kind        string         `gorm:"type:varchar(16);not null"`
	Shape       datatypes.JSON `gorm:"type:jsonb;not null"`
	Ring        string         `gorm:"type:polygon;not null;->:false;<-:false"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null"`
	DeletedAt   *time.Time     `gorm:"type:timestamptz"`
}

func (LeadArea) TableName() string {
	return "lead_areas"
}
