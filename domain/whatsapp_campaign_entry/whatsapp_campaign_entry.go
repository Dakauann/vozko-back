package whatsapp_campaign_entry

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/lead"
)

var (
	ErrEntryNotFound         = errors.New("whatsapp campaign entry: not found")
	ErrEntryCampaignRequired = errors.New("whatsapp campaign entry: campaign id is required")
	ErrEntryLeadRequired     = errors.New("whatsapp campaign entry: lead id is required")
	ErrEntryStatusInvalid    = errors.New("whatsapp campaign entry: status is invalid")
	ErrEntryDuplicate        = errors.New("whatsapp campaign entry: entry already exists for this campaign and lead")
)

// SendStatus is this channel's send status. The type and its values are shared
// with every other channel (domain/campaign); what is channel-specific is WHICH
// of them this channel can produce, declared by StatusSet below.
type SendStatus = campaign.SendStatus

const (
	SendStatusPending                 = campaign.SendStatusPending
	SendStatusSent                    = campaign.SendStatusSent
	SendStatusDelivered               = campaign.SendStatusDelivered
	SendStatusRead                    = campaign.SendStatusRead
	SendStatusFailed                  = campaign.SendStatusFailed
	SendStatusNotEligiblePossibleSpam = campaign.SendStatusNotEligiblePossibleSpam
)

// StatusSet is the closed vocabulary of the Cloud API campaign, and the single
// place a new status gets added.
//
// SKIPPED_NOT_ON_WHATSAPP is deliberately absent: this transport cannot ask
// whether a number is on WhatsApp before sending, so it can never produce that
// answer.
func StatusSet() campaign.StatusSet {
	return campaign.StatusSet{
		All: AllStatuses(),
		// The buckets a workspace is never charged for: not-yet-sent,
		// spam-protection skips, and failed sends. Named once so the SQL that
		// filters billed volume and the domain that counts it cannot drift.
		NonDispatch: []SendStatus{
			SendStatusPending,
			SendStatusFailed,
			SendStatusNotEligiblePossibleSpam,
		},
	}
}

// AllStatuses is the closed set of send statuses this channel produces.
func AllStatuses() []SendStatus {
	return []SendStatus{
		SendStatusPending,
		SendStatusSent,
		SendStatusDelivered,
		SendStatusRead,
		SendStatusFailed,
		SendStatusNotEligiblePossibleSpam,
	}
}

// NonDispatchStatuses are the buckets a workspace is never charged for.
func NonDispatchStatuses() []SendStatus { return StatusSet().NonDispatch }

// DispatchedStatuses are the entries that actually left our system: today
// SENT, DELIVERED and READ. Derived subtractively from AllStatuses for the
// reason spelled out on campaign.Counts.Dispatches — a future billed status
// joins this set automatically instead of being silently dropped from every
// export and filter that asks for "what we sent".
func DispatchedStatuses() []SendStatus { return StatusSet().Dispatched() }

// StatusStrings renders a status set for a repository IN clause.
func StatusStrings(statuses []SendStatus) []string { return campaign.Strings(statuses) }

// ValidStatus reports whether this channel can produce that status.
//
// A package function rather than a method, because SendStatus is now shared
// across channels and validity is not: the same value can be legal on one
// transport and impossible on another.
func ValidStatus(s SendStatus) bool { return StatusSet().Valid(s) }

type WhatsAppCampaignEntry struct {
	ID                      string                 `json:"id"`
	CampaignID              string                 `json:"campaignId"`
	LeadID                  string                 `json:"leadId"`
	Lead                    *lead.Lead             `json:"lead,omitempty"`
	Status                  SendStatus             `json:"status"`
	MessageID               string                 `json:"messageId,omitempty"`
	ErrorCode               int                    `json:"errorCode,omitempty"`
	ErrorMessage            string                 `json:"errorMessage,omitempty"`
	ReceivedBusinessPhoneID string                 `json:"receivedBusinessPhoneId,omitempty"`
	Variables               []string               `json:"variables,omitempty"`
	AutomationEnabled       *bool                  `json:"automationEnabled,omitempty"`
	Metadata                map[string]interface{} `json:"metadata,omitempty"`
	ConversationStatus      string                 `json:"conversationStatus,omitempty"`
	// Close provenance (only meaningful when ConversationStatus is finished).
	CloseSource string     `json:"closeSource,omitempty"`
	CloseReason string     `json:"closeReason,omitempty"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
	// Denormalized clocks for idle auto-close eligibility (maintained on message write).
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`
	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

func (e *WhatsAppCampaignEntry) IsAutomationEnabled() bool {
	if e.AutomationEnabled == nil {
		return true
	}
	return *e.AutomationEnabled
}

func (e *WhatsAppCampaignEntry) Normalize() {
	e.ID = strings.TrimSpace(e.ID)
	e.CampaignID = strings.TrimSpace(e.CampaignID)
	e.LeadID = strings.TrimSpace(e.LeadID)
	e.MessageID = strings.TrimSpace(e.MessageID)
	if !ValidStatus(e.Status) {
		e.Status = SendStatusPending
	}
}

func (e *WhatsAppCampaignEntry) Validate() error {
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

// StatusCounts is the per-status tally. Shared with every channel; see
// campaign.Counts for why Dispatches is defined subtractively.
type StatusCounts = campaign.Counts
