package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Pipeline is the GORM row model for a workspace-global stage set. It is kept
// separate from the domain pipeline.Pipeline entity and hand-mapped in the
// repository. Added to AutoMigrate in migrate.go.
type Pipeline struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	WorkspaceID string `gorm:"type:uuid;not null;index:idx_pipeline_workspace"`
	Name        string `gorm:"not null;size:120"`
	ObjectType  string `gorm:"type:varchar(20);not null;default:conversation;index:idx_pipeline_ws_object"`
	// The stage group this pipeline was stamped from (null = created directly / default).
	// Lets a campaign reusing the same group land on the SAME pipeline instead of
	// forking a duplicate, "same stage group → same board".
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
