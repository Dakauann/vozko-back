package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pipeline struct {
	ID           string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string         `gorm:"type:uuid;not null;index:idx_pipeline_workspace"`
	Name         string         `gorm:"not null;size:120"`
	ObjectType   string         `gorm:"type:varchar(20);not null;default:conversation;index:idx_pipeline_ws_object"`
	StageGroupID string         `gorm:"type:uuid;default:null;index:idx_pipeline_ws_group"`
	DepartmentID string         `gorm:"type:uuid;default:null"`
	Position     int            `gorm:"default:0"`
	IsDefault    bool           `gorm:"default:false"`
	CreatedAt    time.Time      `gorm:"autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (Pipeline) TableName() string {
	return "pipelines"
}

func (p *Pipeline) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
