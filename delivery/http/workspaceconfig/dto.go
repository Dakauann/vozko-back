package workspaceconfig

import (
	"time"

	"vozko/domain/working_hours"
	workspaceconfigdomain "vozko/domain/workspace_config"
)

type WorkspaceConfigResponse struct {
	ID                         string `json:"id" example:"cfg_a1b2c3"`
	WorkspaceID                string `json:"workspaceId" example:"ws_a1b2c3"`
	CampaignSpamProtectionDays int    `json:"campaignSpamProtectionDays" example:"3"`
	SkipAdminAssignment        bool   `json:"skipAdminAssignment" example:"false"`
	// IncludedUnofficialWhatsAppInstances is the platform-granted allowance of
	// linked-device WhatsApp numbers. Readable by the workspace so its own
	// screens can explain why the connect button is disabled; writable only
	// through the /admin route.
	IncludedUnofficialWhatsAppInstances int  `json:"includedUnofficialWhatsAppInstances" example:"2"`
	AutoCloseEnabled                    bool `json:"autoCloseEnabled" example:"true"`
	AutoCloseIdleAfterHours             int  `json:"autoCloseIdleAfterHours" example:"24"`
	AutoCloseMaxAgeEnabled              bool `json:"autoCloseMaxAgeEnabled" example:"true"`
	AutoCloseMaxAgeAfterHours           int  `json:"autoCloseMaxAgeAfterHours" example:"168"`
	// RouletteMode is "online" (distribute among connected agents, the default)
	// or "last_seen" (distribute among everyone online within the window).
	RouletteMode string `json:"rouletteMode" example:"online"`
	// RouletteLastSeenWindowHours is how long after going offline an agent stays
	// in the ring (1..168). Only meaningful in last_seen mode.
	RouletteLastSeenWindowHours int `json:"rouletteLastSeenWindowHours" example:"48"`
	// RouletteRescueEnabled reassigns a conversation whose owner never opened
	// it. Only acts in last_seen mode.
	RouletteRescueEnabled bool `json:"rouletteRescueEnabled" example:"true"`
	// RouletteRescueAfterMinutes is the owner's deadline (1..1440).
	RouletteRescueAfterMinutes int `json:"rouletteRescueAfterMinutes" example:"15"`
	// WorkingHours is the workspace's weekly schedule, or absent when none is
	// set — which means the roulette rescue runs around the clock.
	//
	// While it is set, the rescue is paused outside these hours AND the owner's
	// deadline only counts the minutes inside them, so a conversation handed out
	// five minutes before closing still gets its full deadline the next morning.
	WorkingHours *working_hours.Spec `json:"workingHours,omitempty"`
	UpdatedBy    string              `json:"updatedBy,omitempty" example:"usr_a1b2c3"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

func toWorkspaceConfigResponse(c *workspaceconfigdomain.WorkspaceConfig) WorkspaceConfigResponse {
	return WorkspaceConfigResponse{
		ID:                                  c.ID,
		WorkspaceID:                         c.WorkspaceID,
		CampaignSpamProtectionDays:          c.CampaignSpamProtectionDays,
		SkipAdminAssignment:                 c.SkipAdminAssignment,
		IncludedUnofficialWhatsAppInstances: c.IncludedUnofficialWhatsAppInstances,
		AutoCloseEnabled:                    c.AutoCloseEnabled,
		AutoCloseIdleAfterHours:             c.AutoCloseIdleAfterHours,
		AutoCloseMaxAgeEnabled:              c.AutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:           c.AutoCloseMaxAgeAfterHours,
		RouletteMode:                        c.EffectiveRouletteMode(),
		RouletteLastSeenWindowHours:         int(c.EffectiveRouletteLastSeenWindow() / time.Hour),
		RouletteRescueEnabled:               c.RouletteRescueEnabled,
		RouletteRescueAfterMinutes:          int(c.EffectiveRouletteRescueAfter() / time.Minute),
		WorkingHours:                        c.WorkingHours,
		UpdatedBy:                           c.UpdatedBy,
		CreatedAt:                           c.CreatedAt,
		UpdatedAt:                           c.UpdatedAt,
	}
}
