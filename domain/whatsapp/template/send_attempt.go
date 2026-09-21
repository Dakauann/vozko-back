package template

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vozko/domain/conversation"
)

type SendAttempt struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspaceId"`
	UserID         string `json:"userId,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`

	BusinessPhoneID string            `json:"businessPhoneId"`
	TemplateID      string            `json:"templateId"`
	TemplateName    string            `json:"templateName"`
	Language        string            `json:"language"`
	Category        string            `json:"category"`
	ToNumber        string            `json:"toNumber"`
	CampaignID      *string           `json:"campaignId,omitempty"`
	EntryID         *string           `json:"entryId,omitempty"`
	Status          SendAttemptStatus `json:"status"`

	ChargedMicros     int64  `json:"chargedMicros"`
	ProviderMessageID string `json:"providerMessageId,omitempty"`
	ResponseStatus    int    `json:"responseStatus,omitempty"`
	ErrorCode         int    `json:"errorCode,omitempty"`
	ErrorMessage      string `json:"errorMessage,omitempty"`

	ChargedAt  *time.Time `json:"chargedAt,omitempty"`
	SentAt     *time.Time `json:"sentAt,omitempty"`
	RefundedAt *time.Time `json:"refundedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type SendAttemptStatus string

const (
	SendAttemptPending  SendAttemptStatus = "pending"
	SendAttemptCharged  SendAttemptStatus = "charged"
	SendAttemptSent     SendAttemptStatus = "sent"
	SendAttemptRejected SendAttemptStatus = "rejected"
	SendAttemptUnknown  SendAttemptStatus = "unknown"
	SendAttemptRefunded SendAttemptStatus = "refunded"
)

var legalTransitions = map[SendAttemptStatus][]SendAttemptStatus{
	SendAttemptPending:  {SendAttemptCharged},
	SendAttemptCharged:  {SendAttemptSent, SendAttemptRejected, SendAttemptUnknown, SendAttemptRefunded},
	SendAttemptSent:     {SendAttemptRefunded},
	SendAttemptUnknown:  {SendAttemptSent, SendAttemptRejected, SendAttemptRefunded},
	SendAttemptRejected: {SendAttemptRefunded},
	SendAttemptRefunded: {},
}

func (s SendAttemptStatus) CanTransitionTo(next SendAttemptStatus) bool {
	for _, allowed := range legalTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

func PredecessorsOf(next SendAttemptStatus) []SendAttemptStatus {
	var out []SendAttemptStatus
	for from, allowed := range legalTransitions {
		for _, to := range allowed {
			if to == next {
				out = append(out, from)
			}
		}
	}
	return out
}

func (s SendAttemptStatus) IsTerminal() bool {
	return s == SendAttemptSent || s == SendAttemptRefunded
}

func (s SendAttemptStatus) InFlight() bool {
	return s == SendAttemptPending || s == SendAttemptCharged
}

func (a *SendAttempt) NeedsReconciliation(now time.Time, ttl time.Duration) bool {
	if a == nil {
		return false
	}
	if a.Status != SendAttemptCharged && a.Status != SendAttemptUnknown {
		return false
	}
	return now.Sub(a.UpdatedAt) >= ttl
}

type SendOutcome string

const (
	OutcomeAccepted     SendOutcome = "accepted"
	OutcomeAcceptedNoID SendOutcome = "accepted_no_id"
	OutcomeRejected     SendOutcome = "rejected"
	OutcomeUnknown      SendOutcome = "unknown"
)

func ClassifySendOutcome(out *conversation.SendTextMessageOutput, err error) SendOutcome {
	if err == nil {
		if out != nil && strings.TrimSpace(out.MessageID) != "" {
			return OutcomeAccepted
		}
		return OutcomeAcceptedNoID
	}

	if errors.Is(err, conversation.ErrSendOutcomeUnknown) {
		return OutcomeAcceptedNoID
	}

	if out != nil && out.ResponseStatus >= 400 && out.ResponseStatus < 500 {
		return OutcomeRejected
	}
	if out != nil && out.ResponseStatus >= 200 && out.ResponseStatus < 300 {
		return OutcomeAcceptedNoID
	}
	return OutcomeUnknown
}

func (o SendOutcome) ShouldRefund() bool { return o == OutcomeRejected }

func (o SendOutcome) StatusFor() SendAttemptStatus {
	switch o {
	case OutcomeAccepted, OutcomeAcceptedNoID:
		return SendAttemptSent
	case OutcomeRejected:
		return SendAttemptRejected
	default:
		return SendAttemptUnknown
	}
}

func ChargeReferenceID(attemptID string) string { return "waba:" + attemptID }

func RefundReferenceID(attemptID string) string { return "refund:" + ChargeReferenceID(attemptID) }

func ParseProviderError(out *conversation.SendTextMessageOutput) (int, string) {
	if out == nil || len(out.ResponsePayload) == 0 {
		return 0, ""
	}
	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.ResponsePayload, &parsed); err != nil {
		return 0, ""
	}
	msg := parsed.Error.Message
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return parsed.Error.Code, msg
}
