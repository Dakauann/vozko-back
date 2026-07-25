package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspacePlanDefinition struct {
	ID                             string `gorm:"primaryKey;type:uuid"`
	Name                           string `gorm:"type:varchar(120);not null"`
	Description                    string `gorm:"type:text;not null;default:''"`
	BasePriceBRLCents              int64  `gorm:"not null;default:0"`
	MaxCallChannels                int    `gorm:"not null"`
	IncludedWhatsAppBusinessPhones int    `gorm:"not null;default:0"`
	// MaxBranches (branches/SIP extensions) defaults to 1 so every plan grants one
	// member extension out of the box; an admin raises it per plan. AutoMigrate
	// backfills existing rows (see migrate.go, which also lifts legacy 0 rows to 1).
	MaxBranches int `gorm:"not null;default:1"`
	// MaxHoldMusicTracks (custom hold music uploads) defaults to 3 so the feature
	// works out of the box on every plan; an admin raises or zeroes it per plan.
	// The media layer hard-caps the effective value at 10.
	MaxHoldMusicTracks   int        `gorm:"not null;default:3"`
	IsGloballyVisible    bool       `gorm:"not null;default:true"`
	ExclusiveAffiliateID *string    `gorm:"type:uuid;index"`
	ArchivedAt           *time.Time `gorm:"index"`
	CreatedAt            time.Time  `gorm:"autoCreateTime"`
	UpdatedAt            time.Time  `gorm:"autoUpdateTime"`
}

func (WorkspacePlanDefinition) TableName() string { return "workspace_plan_definitions" }

func (p *WorkspacePlanDefinition) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

type PlanVisibilityEntry struct {
	PlanDefinitionID string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID      string    `gorm:"primaryKey;type:uuid"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
}

func (PlanVisibilityEntry) TableName() string { return "plan_visibility_entries" }
