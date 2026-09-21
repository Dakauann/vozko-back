package workspaceconfig

import (
	"time"

	"vozko/domain/working_hours"
	workspaceconfigdomain "vozko/domain/workspace_config"
)

type WorkspaceConfigResponse struct {
	ID                                  string              `json:"id" example:"cfg_a1b2c3"`
	WorkspaceID                         string              `json:"workspaceId" example:"ws_a1b2c3"`
	CampaignSpamProtectionDays          int                 `json:"campaignSpamProtectionDays" example:"3"`
	SkipAdminAssignment                 bool                `json:"skipAdminAssignment" example:"false"`
	IncludedUnofficialWhatsAppInstances int                 `json:"includedUnofficialWhatsAppInstances" example:"2"`
	AutoCloseEnabled                    bool                `json:"autoCloseEnabled" example:"true"`
	AutoCloseIdleAfterHours             int                 `json:"autoCloseIdleAfterHours" example:"24"`
	AutoCloseMaxAgeEnabled              bool                `json:"autoCloseMaxAgeEnabled" example:"true"`
	AutoCloseMaxAgeAfterHours           int                 `json:"autoCloseMaxAgeAfterHours" example:"168"`
	RouletteMode                        string              `json:"rouletteMode" example:"online"`
	RouletteLastSeenWindowHours         int                 `json:"rouletteLastSeenWindowHours" example:"48"`
	RouletteRescueEnabled               bool                `json:"rouletteRescueEnabled" example:"true"`
	RouletteRescueAfterMinutes          int                 `json:"rouletteRescueAfterMinutes" example:"15"`
	WorkingHours                        *working_hours.Spec `json:"workingHours,omitempty"`
	UpdatedBy                           string              `json:"updatedBy,omitempty" example:"usr_a1b2c3"`
	CreatedAt                           time.Time           `json:"createdAt"`
	UpdatedAt                           time.Time           `json:"updatedAt"`
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
