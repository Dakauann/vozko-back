package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type StudioProject struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"type:uuid;not null;index:idx_studio_projects_listing,priority:1"`
	Kind        string         `gorm:"size:16;not null;index:idx_studio_projects_listing,priority:2"`
	Name        string         `gorm:"size:160;not null"`
	Document    datatypes.JSON `gorm:"type:jsonb;not null"`
	Version     int64          `gorm:"not null;default:1"`
	CreatedBy   string         `gorm:"type:uuid;not null"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime;index:idx_studio_projects_listing,priority:3,sort:desc"`
	ArchivedAt  *time.Time     `gorm:"type:timestamptz"`
}

func (StudioProject) TableName() string { return "studio_projects" }

func (p *StudioProject) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
