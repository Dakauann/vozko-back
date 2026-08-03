package whatsapp_campaign_entry

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/lead"
)

var (
	ErrEntryNotFound         = errors.New("whatsapp campaign entry: not found")
	ErrEntryCampaignRequired = errors.New("whatsapp campaign entry: campaign id is required")
	ErrEntryLeadRequired     = errors.New("whatsapp campaign entry: lead id is required")
	ErrEntryStatusInvalid    = errors.New("whatsapp campaign entry: status is invalid")
	ErrEntryDuplicate        = errors.New("whatsapp campaign entry: entry already exists for this campaign and lead")
)

type SendStatus string

const (
	SendStatusPending                 SendStatus = "PENDING"
	SendStatusSent                    SendStatus = "SENT"
	SendStatusDelivered               SendStatus = "DELIVERED"
	SendStatusRead                    SendStatus = "READ"
	SendStatusFailed                  SendStatus = "FAILED"
	SendStatusNotEligiblePossibleSpam SendStatus = "NOT_ELIGIBLE_POSSIBLE_SPAM"
)

func (s SendStatus) Valid() bool {
	switch s {
	case SendStatusPending, SendStatusSent, SendStatusDelivered, SendStatusRead, SendStatusFailed, SendStatusNotEligiblePossibleSpam:
		return true
	default:
		return false
	}
}

func (s SendStatus) IsTerminal() bool {
	switch s {
	case SendStatusDelivered, SendStatusRead, SendStatusFailed, SendStatusNotEligiblePossibleSpam:
		return true
	default:
		return false
	}
}

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
	if !e.Status.Valid() {
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
	if !e.Status.Valid() {
		return ErrEntryStatusInvalid
	}
	return nil
}

type StatusCounts struct {
	Total                   int64 `json:"total"`
	Pending                 int64 `json:"pending"`
	Sent                    int64 `json:"sent"`
	Delivered               int64 `json:"delivered"`
	Read                    int64 `json:"read"`
	Failed                  int64 `json:"failed"`
	NotEligiblePossibleSpam int64 `json:"notEligiblePossibleSpam"`
}

func (sc StatusCounts) Processed() int64 {
	return sc.Sent + sc.Delivered + sc.Read + sc.Failed + sc.NotEligiblePossibleSpam
}

// Dispatches ("disparos") counts BILLED sends, the entries a workspace was
// actually charged for. It is defined subtractively from billing eligibility,
// NOT by summing the WhatsApp delivery lifecycle (SENT/DELIVERED/READ): we take
// the whole campaign and remove the buckets that are never billed, not-yet-sent
// (PENDING), spam-protection skips (NOT_ELIGIBLE_POSSIBLE_SPAM) and failed sends
// (FAILED). Everything else that left our system counts, so a future billed
// status is included automatically instead of being silently dropped. Today the
// six statuses partition Total, so this equals SENT+DELIVERED+READ; the
// subtractive form is what keeps it correct as statuses evolve.
func (sc StatusCounts) Dispatches() int64 {
	billed := sc.Total - sc.Pending - sc.Failed - sc.NotEligiblePossibleSpam
	if billed < 0 {
		return 0
	}
	return billed
}
