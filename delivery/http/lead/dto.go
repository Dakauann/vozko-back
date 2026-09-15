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

	// SeedInbox opens an empty unofficial WhatsApp conversation for every
	// imported number, so the leads are answerable from the inbox without
	// anyone having to message first.
	//
	// Opt-in, and false by default. It creates a conversation per row on the
	// workspace's oldest connected number, which is a real change to what the
	// inbox contains; a list imported only to be exported later should not get
	// one. Seeding never sends anything.
	SeedInbox bool `json:"seedInbox,omitempty"`

	// SeedConversations writes an example thread into each seeded conversation
	// instead of leaving it blank, using AI and the WORKSPACE's own balance.
	//
	// Nil is off. System administrator only, and only alongside SeedInbox: the
	// two are separate questions (who may spend on this, and who may open cold
	// conversations at all) and the server refuses the combination rather than
	// inferring one from the other.
	//
	// swaggerignore because /swagger is served unauthenticated, to anyone, in
	// production. The handler already refuses this field to non-administrators,
	// so publishing it is not a privilege hole; it is an advertisement that the
	// capability exists and spends the workspace's balance, to an audience that
	// can never use it.
	SeedConversations *SeedConversationsSpec `json:"seedConversations,omitempty" swaggerignore:"true"`
}

// SeedConversationsSpec is the script an administrator wrote for this import.
//
// Carried in the request rather than stored: the act is per import and the
// checkbox is in the import dialog. A saved, reusable template would be a
// table, a CRUD surface and a migration for something nobody asked to reuse.
type SeedConversationsSpec struct {
	// Bodies is the first message, in variants. One is chosen per contact,
	// deterministically from their number, so two hundred seeded conversations
	// do not read as one conversation copied two hundred times.
	Bodies []string `json:"bodies"`
	// MaxMessages is the whole thread's length, counting the first message.
	MaxMessages int `json:"maxMessages" example:"4"`
	// Context is optional free text about what the business sells, so the
	// model has more than a one-line opener to reason from.
	Context string `json:"context,omitempty"`
}

// toDomain converts the wire shape into the domain's script.
//
// A conversion rather than an alias, the same way every other type in this
// package owns its own wire shape: the API decides what it promises, and the
// domain stays free to change its vocabulary.
func (s *SeedConversationsSpec) toDomain() *unofficial_whatsapp.SeedScript {
	if s == nil {
		return nil
	}
	return &unofficial_whatsapp.SeedScript{
		Bodies:      s.Bodies,
		MaxMessages: s.MaxMessages,
		Context:     s.Context,
	}
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

	// InboxSeedQueued is how many numbers were handed to the inbox seeding job,
	// when the request asked for it.
	//
	// Queued, not seeded: the work runs in the background, so this is a promise
	// about what was accepted and not a report of what now exists. The UI has to
	// word it that way, or an operator refreshes the inbox, sees nothing yet and
	// reads a lie.
	InboxSeedQueued int `json:"inboxSeedQueued,omitempty"`
	// InboxSeedError explains why seeding could not even be queued, when the
	// leads themselves imported fine. A separate field rather than a failed
	// response: the import succeeded, and telling the operator otherwise would
	// have them run it again.
	InboxSeedError string `json:"inboxSeedError,omitempty"`

	// ScriptedSeedQueued is how many of those conversations will carry a
	// written example thread.
	//
	// Separate from InboxSeedQueued, and always smaller or equal, because only
	// the first MaxScriptedTargets of an import cost anything. One number could
	// not say "the conversations were queued, and two hundred of them will have
	// a script".
	//
	// swaggerignore for the same reason as the request field: it only ever
	// answers a request a non-administrator cannot make.
	ScriptedSeedQueued int `json:"scriptedSeedQueued,omitempty" swaggerignore:"true"`
	// ScriptedSeedError explains why the conversations will open blank when the
	// import asked for scripted ones.
	//
	// Separate from InboxSeedError for the case that motivates the whole pair:
	// the conversations WERE queued and none of them will have a script,
	// because the caller is not a system administrator.
	//
	// swaggerignore: see above.
	ScriptedSeedError string `json:"scriptedSeedError,omitempty" swaggerignore:"true"`
}

// MaxReportedRejections bounds the rejection list in the response.
//
// Enough to fix a normal file by hand; short of shipping a 20.000-line error
// payload back to a browser when someone imports the wrong column.
const MaxReportedRejections = 200
