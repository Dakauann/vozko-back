package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceAddonDefinition struct {
	ID                 string     `gorm:"primaryKey;type:uuid"`
	Key                string     `gorm:"type:varchar(80);not null;index"`
	Name               string     `gorm:"type:varchar(120);not null"`
	Description        string     `gorm:"type:text;not null;default:''"`
	EntitlementKind    string     `gorm:"type:varchar(40);not null;index"`
	UnitsPerQuantity   int        `gorm:"not null;default:1"`
	MonthlyPriceMicros int64      `gorm:"not null;default:0"`
	AnnualPriceMicros  int64      `gorm:"not null;default:0"`
	MonthlyCostMicros  int64      `gorm:"not null;default:0"`
	AnnualCostMicros   int64      `gorm:"not null;default:0"`
	IsActive           bool       `gorm:"not null;default:true"`
	IsGloballyVisible  bool       `gorm:"not null;default:true"`
	ArchivedAt         *time.Time `gorm:"index"`
	CreatedAt          time.Time  `gorm:"autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime"`
}

func (WorkspaceAddonDefinition) TableName() string { return "workspace_addon_definitions" }

func (d *WorkspaceAddonDefinition) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

type WorkspaceAddonSubscription struct {
	ID                 string     `gorm:"primaryKey;type:uuid"`
	WorkspaceID        string     `gorm:"type:uuid;not null;index"`
	AddonDefinitionID  string     `gorm:"type:uuid;not null;index"`
	AddonKey           string     `gorm:"type:varchar(80);not null"`
	EntitlementKind    string     `gorm:"type:varchar(40);not null;index"`
	Quantity           int        `gorm:"not null;default:1"`
	UnitsPerQuantity   int        `gorm:"not null;default:1"`
	BillingCycle       string     `gorm:"type:varchar(20);not null;default:'monthly'"`
	Status             string     `gorm:"type:varchar(20);not null;index"`
	UnitPriceMicros    int64      `gorm:"not null;default:0"`
	BoundResourceType  *string    `gorm:"type:varchar(40);index"`
	BoundResourceID    *string    `gorm:"type:varchar(120);index"`
	CurrentPeriodStart time.Time  `gorm:"not null;index"`
	CurrentPeriodEnd   time.Time  `gorm:"not null;index"`
	CancelledAt        *time.Time `gorm:"index"`
	CreatedAt          time.Time  `gorm:"autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime"`
}

func (WorkspaceAddonSubscription) TableName() string { return "workspace_addon_subscriptions" }

func (s *WorkspaceAddonSubscription) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
