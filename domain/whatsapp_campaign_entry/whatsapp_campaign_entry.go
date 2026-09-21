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

type SendStatus = campaign.SendStatus

const (
	SendStatusPending                 = campaign.SendStatusPending
	SendStatusSent                    = campaign.SendStatusSent
	SendStatusDelivered               = campaign.SendStatusDelivered
	SendStatusRead                    = campaign.SendStatusRead
	SendStatusFailed                  = campaign.SendStatusFailed
	SendStatusNotEligiblePossibleSpam = campaign.SendStatusNotEligiblePossibleSpam
)

func StatusSet() campaign.StatusSet {
	return campaign.StatusSet{
		All: AllStatuses(),
		NonDispatch: []SendStatus{
			SendStatusPending,
			SendStatusFailed,
			SendStatusNotEligiblePossibleSpam,
		},
	}
}

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

func NonDispatchStatuses() []SendStatus { return StatusSet().NonDispatch }

func DispatchedStatuses() []SendStatus { return StatusSet().Dispatched() }

func StatusStrings(statuses []SendStatus) []string { return campaign.Strings(statuses) }

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
	CloseSource             string                 `json:"closeSource,omitempty"`
	CloseReason             string                 `json:"closeReason,omitempty"`
	ClosedAt                *time.Time             `json:"closedAt,omitempty"`
	LastCustomerMessageAt   *time.Time             `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt      *time.Time             `json:"lastAgentMessageAt,omitempty"`
	LastMessageAt           *time.Time             `json:"lastMessageAt,omitempty"`
	CreatedAt               time.Time              `json:"createdAt"`
	UpdatedAt               time.Time              `json:"updatedAt"`
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

type StatusCounts = campaign.Counts
