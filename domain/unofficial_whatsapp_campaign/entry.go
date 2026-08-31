package unofficial_whatsapp_campaign

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
)

var (
	ErrEntryNotFound         = errors.New("unofficial whatsapp campaign entry not found")
	ErrEntryCampaignRequired = errors.New("unofficial whatsapp campaign entry: campaign id is required")
	ErrEntryLeadRequired     = errors.New("unofficial whatsapp campaign entry: lead id is required")
	ErrEntryStatusInvalid    = errors.New("unofficial whatsapp campaign entry: status is invalid")
	ErrEntryDuplicate        = errors.New("this number is already in the campaign")
)

// StatusSet is this channel's closed vocabulary, and the single place a new
// status gets added.
//
// It is the official channel's set plus SKIPPED_NOT_ON_WHATSAPP, which only
// exists here because only here can a number be checked against WhatsApp before
// anything is sent. It sits in NonDispatch alongside the spam bucket: nothing
// was transmitted, so it is not a dispatch — and it is deliberately not FAILED,
// because a dead number is a fact about the list, not about our sending.
func StatusSet() campaign.StatusSet {
	return campaign.StatusSet{
		All: []campaign.SendStatus{
			campaign.SendStatusPending,
			campaign.SendStatusSent,
			campaign.SendStatusDelivered,
			campaign.SendStatusRead,
			campaign.SendStatusFailed,
			campaign.SendStatusNotEligiblePossibleSpam,
			campaign.SendStatusSkippedNotOnWhatsApp,
		},
		NonDispatch: []campaign.SendStatus{
			campaign.SendStatusPending,
			campaign.SendStatusFailed,
			campaign.SendStatusNotEligiblePossibleSpam,
			campaign.SendStatusSkippedNotOnWhatsApp,
		},
	}
}

// ValidStatus reports whether this channel can produce that status.
func ValidStatus(s campaign.SendStatus) bool { return StatusSet().Valid(s) }

// NumberCheckTTL is how long a /chat/check answer is trusted.
//
// A window rather than "check once" because registration changes: a number that
// was dead last month can be live today, and a campaign resumed weeks later
// would otherwise skip people who are now reachable. A week is short enough to
// stay accurate and long enough that a resumed campaign does not re-verify a
// list it verified this morning.
const NumberCheckTTL = 7 * 24 * time.Hour

// Entry is one target of a campaign.
//
// It POINTS AT a conversation rather than being one. That is the decisive
// divergence from the Cloud API campaign, where the entry IS the conversation
// row: here an inbound webhook carries no campaign, so if the same person were
// targeted by two campaigns there would be no way to decide which transcript to
// append their reply to. One real WhatsApp chat is one CRM conversation, and a
// campaign records its attribution on this row instead.
//
// Note what is deliberately ABSENT: conversationStatus, closeSource, closedAt,
// automationEnabled, lastCustomerMessageAt. All of those live on
// unofficial_whatsapp_conversations, which already owns them; duplicating them
// here would create two answers to "is this chat finished".
type Entry struct {
	ID          string `json:"id"`
	CampaignID  string `json:"campaignId"`
	WorkspaceID string `json:"workspaceId"`

	// LeadID is the CRM bridge, resolved at import through the same
	// FindOrCreateMany the official campaign uses. It is what makes these
	// contacts reachable by exports, boletos and every other lead-keyed tool.
	LeadID string `json:"leadId"`
	Number string `json:"number"`
	Name   string `json:"name,omitempty"`

	// Resolved lazily at send time; these are what make a campaign row
	// clickable through to a real transcript.
	ContactID      string `json:"contactId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`

	// JID and CheckedAt cache the registration lookup. A nil CheckedAt means
	// never checked, and a never-checked number is never sent to.
	JID       string     `json:"jid,omitempty"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`

	Status campaign.SendStatus `json:"status"`

	// VariantIndex records WHICH body this recipient received. Without it a
	// workspace running variants has no way to tell which one performed, and
	// the feature becomes noise rather than a measurement.
	VariantIndex int `json:"variantIndex"`

	ProviderMessageID string `json:"providerMessageId,omitempty"`
	// MessageID is our conversation_messages row, linking the entry to the exact
	// bubble in the transcript.
	MessageID    string `json:"messageId,omitempty"`
	ErrorCode    int    `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`

	SentAt    *time.Time `json:"sentAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (e *Entry) Normalize() {
	e.ID = strings.TrimSpace(e.ID)
	e.CampaignID = strings.TrimSpace(e.CampaignID)
	e.LeadID = strings.TrimSpace(e.LeadID)
	e.Number = uw.NormalizePhone(e.Number)
	e.Name = strings.TrimSpace(e.Name)
	e.JID = strings.TrimSpace(e.JID)
	e.ProviderMessageID = strings.TrimSpace(e.ProviderMessageID)
	if !ValidStatus(e.Status) {
		e.Status = campaign.SendStatusPending
	}
}

func (e *Entry) Validate() error {
	if e.CampaignID == "" {
		return ErrEntryCampaignRequired
	}
	if e.LeadID == "" {
		return ErrEntryLeadRequired
	}
	if !ValidStatus(e.Status) {
		return ErrEntryStatusInvalid
	}
	return nil
}

// NeedsNumberCheck reports whether this entry's registration answer is missing
// or stale.
func (e *Entry) NeedsNumberCheck(now time.Time) bool {
	if e.JID == "" || e.CheckedAt == nil {
		return true
	}
	return now.Sub(*e.CheckedAt) > NumberCheckTTL
}

// EntryWithLead is one entry joined to its CRM lead, the shape the entries
// table renders.
type EntryWithLead struct {
	Entry    *Entry      `json:"entry"`
	LeadID   string      `json:"leadId"`
	Number   string      `json:"number"`
	Name     string      `json:"name,omitempty"`
	Age      *int        `json:"age,omitempty"`
	Metadata interface{} `json:"metadata,omitempty"`
	// ConversationStatus and LastMessageAt come from the conversation the entry
	// points at, not from the entry, because that is where they live.
	ConversationStatus string     `json:"conversationStatus,omitempty"`
	LastMessageAt      *time.Time `json:"lastMessageAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`
}
