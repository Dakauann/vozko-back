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
	// IncludedUnofficialWhatsAppInstances is the platform-granted allowance of
	// linked-device WhatsApp numbers. Default 0 — "none included" — so an
	// existing workspace is not silently handed capacity nobody decided to give
	// it when this column appears.
	//
	// The explicit column name is load-bearing, not style, and is the same trap
	// the unofficial-WhatsApp schema documents for `jid`: GORM's naming strategy
	// runs a common-initialism replacer before snake-casing, so "WhatsApp"
	// derives `whats_app` and the column would be
	// `included_unofficial_whats_app_instances`. Any hand-written SQL that spells
	// it the obvious way then fails with "column does not exist".
	IncludedUnofficialWhatsAppInstances int `gorm:"column:included_unofficial_whatsapp_instances;type:int;not null;default:0"`
	// NOTE: hold_music_track and the four queue_* columns (queue_enabled,
	// queue_max_wait_seconds, queue_max_length, queue_overflow) are deliberately
	// orphaned. Hold music and the ACD waiting line went out with SIP telephony,
	// and AutoMigrate never DROPs, so the columns stay behind unread.

	// Conversation auto-close: enabled by default; idle hours after last agent/AI message.
	AutoCloseEnabled        bool `gorm:"not null;default:true"`
	AutoCloseIdleAfterHours int  `gorm:"type:int;not null;default:24"`
	// Absolute inactivity (any last message). Default on; 168h (7d). Cap 90d.
	AutoCloseMaxAgeEnabled    bool `gorm:"not null;default:true"`
	AutoCloseMaxAgeAfterHours int  `gorm:"type:int;not null;default:168"`

	// Roulette distribution policy. The default is the historical behaviour
	// ('online'), so AutoMigrate adding these columns changes nothing for any
	// existing workspace — the feature ships dark until an admin opts in.
	//
	// Rescue defaults ON but only acts in last_seen mode (see
	// WorkspaceConfig.RouletteRescueActive), which is what keeps the default
	// true here from being a behaviour change for everyone.
	RouletteMode                string `gorm:"type:varchar(16);not null;default:'online'"`
	RouletteLastSeenWindowHours int    `gorm:"type:int;not null;default:48"`
	RouletteRescueEnabled       bool   `gorm:"not null;default:true"`
	RouletteRescueAfterMinutes  int    `gorm:"type:int;not null;default:15"`

	// WorkingHours is the workspace's weekly open/closed schedule, stored as the
	// working_hours.Spec document.
	//
	// NULL — not a JSON null, an absent column value — means no working hours
	// are configured, which the domain reads as always open. That is what keeps
	// this additive: every existing row stays NULL and every existing workspace
	// keeps distributing and rescuing around the clock exactly as before.
	WorkingHours *string `gorm:"type:jsonb"`

	// Audience analysis, decided per WORKSPACE rather than per channel account.
	//
	// AudienceDailyCap is the rolling 24h volume ceiling; AudienceDebounceMinutes
	// is how long a conversation must stay quiet before it is handed to the
	// engine. Both default to 0, meaning "not set", which resolves to the
	// per-account ceiling and to audience.DefaultDebounceMinutes respectively, so
	// these columns appearing changes nothing for an existing row.
	//
	// They live here because this is the workspace's configuration row, but they
	// are NOT reachable from the workspace-config screen's input types: the
	// audience use case owns them and writes them under audience:update, which is
	// the permission that governs everything else about analysis. Mapping them in
	// Get and Upsert below is what keeps the two writers from clobbering each
	// other, since that update path is read-modify-write.
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
