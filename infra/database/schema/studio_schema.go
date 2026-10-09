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

type StudioCapabilityReport struct {
	SessionID       string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID     string         `gorm:"type:uuid;not null;index"`
	UserID          string         `gorm:"type:uuid;not null"`
	Kind            string         `gorm:"size:16;not null"`
	Backend         string         `gorm:"size:16;not null;index"`
	GPUVendor       string         `gorm:"column:gpu_vendor;size:200"`
	GPURenderer     string         `gorm:"column:gpu_renderer;size:200"`
	WebGPU          bool           `gorm:"column:webgpu;not null;default:false"`
	Decode          bool           `gorm:"not null;default:false"`
	EncodeVideo     bool           `gorm:"not null;default:false"`
	EncodeAudio     bool           `gorm:"not null;default:false"`
	PixelRatio      float64        `gorm:"not null;default:0"`
	Cores           int            `gorm:"not null;default:0"`
	MemoryGB        float64        `gorm:"column:memory_gb;not null;default:0"`
	UserAgent       string         `gorm:"size:400"`
	Frames          int64          `gorm:"not null;default:0"`
	SlowFrames      int64          `gorm:"not null;default:0"`
	Stalls          int64          `gorm:"not null;default:0"`
	ContextLosses   int64          `gorm:"not null;default:0"`
	DecodeFallbacks int64          `gorm:"not null;default:0"`
	WorkerFailures  int64          `gorm:"not null;default:0"`
	BrowserExports  int64          `gorm:"not null;default:0"`
	ExportFailures  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt       time.Time      `gorm:"autoCreateTime;index"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime"`
}

func (StudioCapabilityReport) TableName() string { return "studio_capability_reports" }
