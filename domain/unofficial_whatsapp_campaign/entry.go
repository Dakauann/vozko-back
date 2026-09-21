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

func ValidStatus(s campaign.SendStatus) bool { return StatusSet().Valid(s) }

const NumberCheckTTL = 7 * 24 * time.Hour

type Entry struct {
	ID          string `json:"id"`
	CampaignID  string `json:"campaignId"`
	WorkspaceID string `json:"workspaceId"`

	LeadID string `json:"leadId"`
	Number string `json:"number"`
	Name   string `json:"name,omitempty"`

	ContactID      string `json:"contactId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`

	JID       string     `json:"jid,omitempty"`
	CheckedAt *time.Time `json:"checkedAt,omitempty"`

	Status campaign.SendStatus `json:"status"`

	VariantIndex int `json:"variantIndex"`

	ProviderMessageID string `json:"providerMessageId,omitempty"`
	MessageID         string `json:"messageId,omitempty"`
	ErrorCode         int    `json:"errorCode,omitempty"`
	ErrorMessage      string `json:"errorMessage,omitempty"`

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

func (e *Entry) NeedsNumberCheck(now time.Time) bool {
	if e.JID == "" || e.CheckedAt == nil {
		return true
	}
	return now.Sub(*e.CheckedAt) > NumberCheckTTL
}

type EntryWithLead struct {
	Entry              *Entry      `json:"entry"`
	LeadID             string      `json:"leadId"`
	Number             string      `json:"number"`
	Name               string      `json:"name,omitempty"`
	Age                *int        `json:"age,omitempty"`
	Metadata           interface{} `json:"metadata,omitempty"`
	ConversationStatus string      `json:"conversationStatus,omitempty"`
	LastMessageAt      *time.Time  `json:"lastMessageAt,omitempty"`
	AutomationEnabled  *bool       `json:"automationEnabled,omitempty"`
}
