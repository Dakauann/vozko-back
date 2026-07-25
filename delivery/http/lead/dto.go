package lead

import (
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
)

type BlockLeadRequest struct {
	Blocked         bool   `json:"blocked,omitempty" example:"true"`
	BusinessPhoneID string `json:"businessPhoneId,omitempty" example:"bp_a1b2c3"`
}

type EntryResponse struct {
	ID         string           `json:"id"`
	CampaignID string           `json:"campaignId"`
	EntryType  shared.EntryType `json:"entryType"`
	Status     string           `json:"status"`
	CreatedAt  string           `json:"createdAt"`
}

type LeadListResponseItem struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspaceId"`
	Number             string  `json:"number"`
	Name               string  `json:"name,omitempty"`
	Age                *int    `json:"age,omitempty"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt"`
	WhatsAppCampaigns  int     `json:"whatsappCampaigns"`
	TotalCampaigns     int     `json:"totalCampaigns"`
	LastActivityAt     *string `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool    `json:"whatsappWindowOpen"`
	WindowExpiresAt    *string `json:"windowExpiresAt,omitempty"`
}

type LeadDetailResponse struct {
	ID                 string                `json:"id"`
	WorkspaceID        string                `json:"workspaceId"`
	Number             string                `json:"number"`
	Name               string                `json:"name,omitempty"`
	Age                *int                  `json:"age,omitempty"`
	Blocked            bool                  `json:"blocked"`
	BlockedAt          *string               `json:"blockedAt,omitempty"`
	BlockedBy          *string               `json:"blockedBy,omitempty"`
	CreatedAt          string                `json:"createdAt"`
	UpdatedAt          string                `json:"updatedAt"`
	WhatsAppCampaigns  int                   `json:"whatsappCampaigns"`
	TotalCampaigns     int                   `json:"totalCampaigns"`
	LastActivityAt     *string               `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool                  `json:"whatsappWindowOpen"`
	WindowExpiresAt    *string               `json:"windowExpiresAt,omitempty"`
	Campaigns          []CampaignHistoryItem `json:"campaigns"`
}

type CampaignHistoryItem struct {
	CampaignID   string              `json:"campaignId"`
	CampaignName string              `json:"campaignName"`
	Type         string              `json:"type"`
	Entries      []CampaignEntryItem `json:"entries"`
}

type CampaignEntryItem struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func toLeadListItem(lws *leaddomain.LeadWithSummary) LeadListResponseItem {
	l := lws.Lead
	s := lws.Summary
	return LeadListResponseItem{
		ID:                 l.ID,
		WorkspaceID:        l.WorkspaceID,
		Number:             l.Number,
		Name:               l.Name,
		Age:                l.Age,
		CreatedAt:          fmtRFC3339(l.CreatedAt),
		UpdatedAt:          fmtRFC3339(l.UpdatedAt),
		WhatsAppCampaigns:  s.WhatsAppCampaigns,
		TotalCampaigns:     s.TotalCampaigns,
		LastActivityAt:     fmtTimePtr(s.LastActivityAt),
		WhatsAppWindowOpen: s.WhatsAppWindowOpen,
		WindowExpiresAt:    fmtTimePtr(s.WindowExpiresAt),
	}
}
