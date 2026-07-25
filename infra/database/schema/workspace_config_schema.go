package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WorkspaceConfig struct {
	ID                         string `gorm:"primaryKey;type:uuid"`
	WorkspaceID                string `gorm:"type:uuid;not null;uniqueIndex:idx_wsc_workspace"`
	CampaignSpamProtectionDays int    `gorm:"type:int;not null;default:3"`
	SkipAdminAssignment        bool   `gorm:"not null;default:false"`
	// HoldMusicTrack: "" | "builtin:<key>" | hold_music media uuid. varchar (not
	// uuid) because builtin keys and the empty default are not uuids.
	HoldMusicTrack string `gorm:"type:varchar(80);not null;default:''"`

	// Call-queue (ACD) policy. 0 for the numeric bounds means "use the server
	// default" (resolved + hard-capped when the policy is read). AutoMigrate adds
	// these columns non-destructively on existing rows.
	QueueEnabled        bool   `gorm:"not null;default:false"`
	QueueMaxWaitSeconds int    `gorm:"type:int;not null;default:0"`
	QueueMaxLength      int    `gorm:"type:int;not null;default:0"`
	QueueOverflow       string `gorm:"type:varchar(16);not null;default:'hangup'"`

	// Conversation auto-close: enabled by default; idle hours after last agent/AI message.
	AutoCloseEnabled        bool `gorm:"not null;default:true"`
	AutoCloseIdleAfterHours int  `gorm:"type:int;not null;default:24"`
	// Absolute inactivity (any last message). Default on; 168h (7d). Cap 90d.
	AutoCloseMaxAgeEnabled    bool `gorm:"not null;default:true"`
	AutoCloseMaxAgeAfterHours int  `gorm:"type:int;not null;default:168"`

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
