package workspaceconfig

import (
	"time"

	workspaceconfigdomain "vozko/domain/workspace_config"
)

type WorkspaceConfigResponse struct {
	ID                         string    `json:"id" example:"cfg_a1b2c3"`
	WorkspaceID                string    `json:"workspaceId" example:"ws_a1b2c3"`
	CampaignSpamProtectionDays int       `json:"campaignSpamProtectionDays" example:"3"`
	SkipAdminAssignment        bool      `json:"skipAdminAssignment" example:"false"`
	HoldMusicTrack             string    `json:"holdMusicTrack" example:"builtin:bossa_nova"`
	QueueEnabled               bool      `json:"queueEnabled" example:"true"`
	QueueMaxWaitSeconds        int       `json:"queueMaxWaitSeconds" example:"120"`
	QueueMaxLength             int       `json:"queueMaxLength" example:"10"`
	QueueOverflow              string    `json:"queueOverflow" example:"recall"`
	AutoCloseEnabled           bool      `json:"autoCloseEnabled" example:"true"`
	AutoCloseIdleAfterHours    int       `json:"autoCloseIdleAfterHours" example:"24"`
	AutoCloseMaxAgeEnabled     bool      `json:"autoCloseMaxAgeEnabled" example:"true"`
	AutoCloseMaxAgeAfterHours  int       `json:"autoCloseMaxAgeAfterHours" example:"168"`
	UpdatedBy                  string    `json:"updatedBy,omitempty" example:"usr_a1b2c3"`
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`
}

func toWorkspaceConfigResponse(c *workspaceconfigdomain.WorkspaceConfig) WorkspaceConfigResponse {
	return WorkspaceConfigResponse{
		ID:                         c.ID,
		WorkspaceID:                c.WorkspaceID,
		CampaignSpamProtectionDays: c.CampaignSpamProtectionDays,
		SkipAdminAssignment:        c.SkipAdminAssignment,
		HoldMusicTrack:             c.HoldMusicTrack,
		QueueEnabled:               c.QueueEnabled,
		QueueMaxWaitSeconds:        c.QueueMaxWaitSeconds,
		QueueMaxLength:             c.QueueMaxLength,
		QueueOverflow:              c.QueueOverflow,
		AutoCloseEnabled:           c.AutoCloseEnabled,
		AutoCloseIdleAfterHours:    c.AutoCloseIdleAfterHours,
		AutoCloseMaxAgeEnabled:     c.AutoCloseMaxAgeEnabled,
		AutoCloseMaxAgeAfterHours:  c.AutoCloseMaxAgeAfterHours,
		UpdatedBy:                  c.UpdatedBy,
		CreatedAt:                  c.CreatedAt,
		UpdatedAt:                  c.UpdatedAt,
	}
}
