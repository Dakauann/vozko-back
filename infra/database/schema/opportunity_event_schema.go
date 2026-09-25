package schema

import (
	"time"

	"gorm.io/datatypes"
)

type OpportunityEvent struct {
	ID            string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID   string         `gorm:"type:uuid;not null;index:idx_opp_event_workspace"`
	OpportunityID string         `gorm:"type:uuid;not null;index:idx_opp_event_opportunity,priority:1"`
	Type          string         `gorm:"type:varchar(24);not null"`
	ActorID       string         `gorm:"type:uuid;default:null"`
	ActorKind     string         `gorm:"type:varchar(16);not null"`
	FromStageID   string         `gorm:"type:uuid;default:null"`
	ToStageID     string         `gorm:"type:uuid;default:null"`
	ValueCents    int64          `gorm:"type:bigint;not null;default:0"`
	Currency      string         `gorm:"type:varchar(3);not null"`
	Details       datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt     time.Time      `gorm:"not null;index:idx_opp_event_opportunity,priority:2"`
}

func (OpportunityEvent) TableName() string {
	return "opportunity_events"
}
