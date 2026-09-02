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

	// Memories are on the row, not one click away: the count is what makes
	// "we know 6 things about this person" visible while scanning, and it is
	// the same number the memory filters segment on.
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

// RenameLeadRequest is the operator-entered name for a lead.
//
// Name is a *string so "not sent" and "sent as empty" stay distinguishable: an
// empty string is a real instruction here — clear the name, show the number —
// and a missing field is a malformed request, not a silent clear.
type RenameLeadRequest struct {
	Name *string `json:"name"`
}

// ImportLeadsRequest is a parsed contact list on its way in.
//
// Rows arrive already split into fields, not as a CSV blob: the browser parses
// the file so the operator can SEE what will happen before committing, and a
// second parser on this side would eventually disagree with that preview about
// what a quoted field or a stray separator means. The file itself never leaves
// the operator's machine.
type ImportLeadsRequest struct {
	Rows []ImportLeadRow `json:"rows"`

	// OnExisting is "fill_empty" (default) or "skip". Never an overwrite:
	// see lead.ExistingPolicy.
	OnExisting string `json:"onExisting,omitempty" example:"fill_empty"`
}

// ImportLeadRow is one line of the operator's file.
//
// Line is the row number in THEIR spreadsheet, echoed back on every rejection
// so a failure points at something they can actually open and fix.
type ImportLeadRow struct {
	Line   int    `json:"line" example:"42"`
	Number string `json:"number" example:"5511987654321"`
	Name   string `json:"name,omitempty" example:"Ana Maria"`
	Age    *int   `json:"age,omitempty" example:"34"`
}

// ImportRejection is one row that did not make it in.
//
// Mirrors leaddomain.Rejection rather than embedding it, the same way every
// other response type here owns its own wire shape: the delivery layer decides
// what the API promises, and the domain stays free to change its vocabulary.
type ImportRejection struct {
	Line   int    `json:"line" example:"42"`
	Number string `json:"number" example:"11987"`
	// Reason is "invalid" (not a reachable number) or "duplicate" (repeated
	// earlier in the same file).
	Reason string `json:"reason" example:"invalid"`
}

// ImportLeadsResponse is the outcome, counted.
type ImportLeadsResponse struct {
	// Created: leads that did not exist in this workspace before.
	Created int64 `json:"created"`
	// Matched: rows whose number the workspace already knew.
	Matched int64 `json:"matched"`
	// Blocked: how many of the matched are blocked, and so unreachable by a
	// campaign built on this list.
	Blocked int64 `json:"blocked"`
	// Invalid: rows whose number is not a reachable phone number.
	Invalid int `json:"invalid"`
	// Duplicate: rows repeating a number that appeared earlier in the SAME file.
	Duplicate int `json:"duplicate"`

	// Rejected lists the rows that did not make it, capped at
	// MaxReportedRejections. Rejections are reported, never silently dropped:
	// an operator told "480 importados" out of 500 with no list of the other 20
	// has a file they cannot repair.
	Rejected []ImportRejection `json:"rejected"`
	// RejectedTruncated says the list above was cut, so the UI can say "+N"
	// rather than implying it showed everything.
	RejectedTruncated int `json:"rejectedTruncated,omitempty"`
}

// MaxReportedRejections bounds the rejection list in the response.
//
// Enough to fix a normal file by hand; short of shipping a 20.000-line error
// payload back to a browser when someone imports the wrong column.
const MaxReportedRejections = 200
