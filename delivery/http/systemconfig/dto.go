package systemconfig

import (
	"vozko/domain/config"
)

type SystemConfigResponse struct {
	ID                     string  `json:"id"`
	BaseSystemPrompt       string  `json:"baseSystemPrompt"`
	MaxConcurrentCalls     int     `json:"maxConcurrentCalls"`
	WorkTimeEnabled        bool    `json:"workTimeEnabled"`
	WorkTimeStart          string  `json:"workTimeStart"`
	WorkTimeEnd            string  `json:"workTimeEnd"`
	AffiliateCommissionPct float64 `json:"affiliateCommissionPct"`
	UpdatedBy              string  `json:"updatedBy"`
	UpdatedAt              string  `json:"updatedAt"`
}

func toSystemConfigResponse(c *config.SystemConfig) SystemConfigResponse {
	return SystemConfigResponse{
		ID:                     c.ID,
		BaseSystemPrompt:       c.BaseSystemPrompt,
		MaxConcurrentCalls:     c.MaxConcurrentCalls,
		WorkTimeEnabled:        c.WorkTimeEnabled,
		WorkTimeStart:          c.WorkTimeStart,
		WorkTimeEnd:            c.WorkTimeEnd,
		AffiliateCommissionPct: c.AffiliateCommissionPct,
		UpdatedBy:              c.UpdatedBy,
		UpdatedAt:              c.UpdatedAt,
	}
}
