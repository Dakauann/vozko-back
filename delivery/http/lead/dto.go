package lead

import (
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/unofficial_whatsapp"
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
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspaceId"`
	Number            string  `json:"number"`
	Name              string  `json:"name,omitempty"`
	ProfilePictureURL string  `json:"profilePictureUrl,omitempty"`
	Age               *int    `json:"age,omitempty"`
	Blocked           bool    `json:"blocked"`
	BlockedAt         *string `json:"blockedAt,omitempty"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`

	WhatsAppCampaigns  int     `json:"whatsappCampaigns"`
	TotalCampaigns     int     `json:"totalCampaigns"`
	LastActivityAt     *string `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool    `json:"whatsappWindowOpen"`
	WindowExpiresAt    *string `json:"windowExpiresAt,omitempty"`

	Memories     int     `json:"memories"`
	LastMemoryAt *string `json:"lastMemoryAt,omitempty"`
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
	item := LeadListResponseItem{
		ID:                 l.ID,
		WorkspaceID:        l.WorkspaceID,
		Number:             l.Number,
		Name:               l.Name,
		ProfilePictureURL:  l.ProfilePictureURL,
		Age:                l.Age,
		Blocked:            l.Blocked,
		CreatedAt:          fmtRFC3339(l.CreatedAt),
		UpdatedAt:          fmtRFC3339(l.UpdatedAt),
		WhatsAppCampaigns:  s.WhatsAppCampaigns,
		TotalCampaigns:     s.TotalCampaigns,
		LastActivityAt:     fmtTimePtr(s.LastActivityAt),
		WhatsAppWindowOpen: s.WhatsAppWindowOpen,
		WindowExpiresAt:    fmtTimePtr(s.WindowExpiresAt),
		Memories:           s.Memories,
		LastMemoryAt:       fmtTimePtr(s.LastMemoryAt),
	}
	if l.Blocked && !l.BlockedAt.IsZero() {
		blockedAt := fmtRFC3339(l.BlockedAt)
		item.BlockedAt = &blockedAt
	}
	return item
}

type RenameLeadRequest struct {
	Name *string `json:"name"`
}

type ImportLeadsRequest struct {
	Rows []ImportLeadRow `json:"rows"`

	OnExisting string `json:"onExisting,omitempty" example:"fill_empty"`

	SeedInbox bool `json:"seedInbox,omitempty"`

	SeedConversations *SeedConversationsSpec `json:"seedConversations,omitempty" swaggerignore:"true"`
}

type SeedConversationsSpec struct {
	Bodies      []string            `json:"bodies"`
	MaxMessages int                 `json:"maxMessages" example:"4"`
	Context     string              `json:"context,omitempty"`
	Attachment  *SeedAttachmentSpec `json:"attachment,omitempty"`
}

type SeedAttachmentSpec struct {
	MediaID string `json:"mediaId"`
	Kind    string `json:"kind" example:"image"`
}

func (a *SeedAttachmentSpec) toDomain() *unofficial_whatsapp.SeedAttachment {
	if a == nil {
		return nil
	}
	return &unofficial_whatsapp.SeedAttachment{
		MediaID: a.MediaID,
		Kind:    unofficial_whatsapp.MediaKind(a.Kind),
	}
}

func (s *SeedConversationsSpec) toDomain() *unofficial_whatsapp.SeedScript {
	if s == nil {
		return nil
	}
	return &unofficial_whatsapp.SeedScript{
		Bodies:      s.Bodies,
		MaxMessages: s.MaxMessages,
		Context:     s.Context,
		Attachment:  s.Attachment.toDomain(),
	}
}

type ImportLeadRow struct {
	Line   int    `json:"line" example:"42"`
	Number string `json:"number" example:"5511987654321"`
	Name   string `json:"name,omitempty" example:"Ana Maria"`
	Age    *int   `json:"age,omitempty" example:"34"`
}

type ImportRejection struct {
	Line   int    `json:"line" example:"42"`
	Number string `json:"number" example:"11987"`
	Reason string `json:"reason" example:"invalid"`
}

type ImportLeadsResponse struct {
	Created   int64 `json:"created"`
	Matched   int64 `json:"matched"`
	Blocked   int64 `json:"blocked"`
	Invalid   int   `json:"invalid"`
	Duplicate int   `json:"duplicate"`

	Rejected          []ImportRejection `json:"rejected"`
	RejectedTruncated int               `json:"rejectedTruncated,omitempty"`

	InboxSeedQueued int    `json:"inboxSeedQueued,omitempty"`
	InboxSeedError  string `json:"inboxSeedError,omitempty"`

	ScriptedSeedQueued int    `json:"scriptedSeedQueued,omitempty" swaggerignore:"true"`
	ScriptedSeedError  string `json:"scriptedSeedError,omitempty" swaggerignore:"true"`
}

const MaxReportedRejections = 200
