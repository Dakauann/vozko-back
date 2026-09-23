package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceConfig struct {
	ID                                  string `gorm:"primaryKey;type:uuid"`
	WorkspaceID                         string `gorm:"type:uuid;not null;uniqueIndex:idx_wsc_workspace"`
	CampaignSpamProtectionDays          int    `gorm:"type:int;not null;default:3"`
	SkipAdminAssignment                 bool   `gorm:"not null;default:false"`
	IncludedUnofficialWhatsAppInstances int    `gorm:"column:included_unofficial_whatsapp_instances;type:int;not null;default:0"`

	AutoCloseEnabled          bool `gorm:"not null;default:true"`
	AutoCloseIdleAfterHours   int  `gorm:"type:int;not null;default:24"`
	AutoCloseMaxAgeEnabled    bool `gorm:"not null;default:true"`
	AutoCloseMaxAgeAfterHours int  `gorm:"type:int;not null;default:168"`

	RouletteMode                string `gorm:"type:varchar(16);not null;default:'online'"`
	RouletteLastSeenWindowHours int    `gorm:"type:int;not null;default:48"`
	RouletteRescueEnabled       bool   `gorm:"not null;default:true"`
	RouletteRescueAfterMinutes  int    `gorm:"type:int;not null;default:15"`

	WorkingHours   *string `gorm:"type:jsonb"`
	OutcomeCapture *string `gorm:"type:jsonb"`

	AudienceDailyCap        int `gorm:"not null;default:0"`
	AudienceDebounceMinutes int `gorm:"not null;default:0"`

	UpdatedBy string    `gorm:"type:uuid"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (WorkspaceConfig) TableName() string {
	return "workspace_configs"
}

func (c *WorkspaceConfig) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}
