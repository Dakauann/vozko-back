package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspacePlanDefinition struct {
	ID                             string     `gorm:"primaryKey;type:uuid"`
	Name                           string     `gorm:"type:varchar(120);not null"`
	Description                    string     `gorm:"type:text;not null;default:''"`
	BasePriceBRLCents              int64      `gorm:"not null;default:0"`
	MaxCallChannels                int        `gorm:"not null"`
	IncludedWhatsAppBusinessPhones int        `gorm:"not null;default:0"`
	MaxBranches                    int        `gorm:"not null;default:1"`
	IsGloballyVisible              bool       `gorm:"not null;default:true"`
	ExclusiveAffiliateID           *string    `gorm:"type:uuid;index"`
	ArchivedAt                     *time.Time `gorm:"index"`
	CreatedAt                      time.Time  `gorm:"autoCreateTime"`
	UpdatedAt                      time.Time  `gorm:"autoUpdateTime"`
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
