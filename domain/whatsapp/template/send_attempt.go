package template

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vozko/domain/conversation"
)

// A SendAttempt is the record of one paid template send, and the reason this
// package can promise "charged exactly once".
//
// Before it existed, the only thing standing between a customer and a double
// charge was the caller remembering not to call twice. The balance ledger cannot
// help: its reference_id is a description on a non-unique index, so two debits
// with the same reference are simply two debits. The queue redelivers. The
// browser retries. Neither is a bug, and both used to cost money.
//
// So the attempt row is the gate. It is written FIRST, under a unique index on
// (workspace_id, idempotency_key), and the send is what one writer does after
// winning that insert. Everything else in this file exists to make that race
// legible: a status that only moves forwards, and a classification of what the
// provider actually did.
type SendAttempt struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	// UserID is the operator who spent the money. The balance ledger has no
	// column for a person, so this row is the only place a charge can be
	// attributed to the human who caused it.
	UserID string `json:"userId,omitempty"`
	// IdempotencyKey is what makes two requests the same request. It comes from
	// the caller — an HTTP header, a campaign entry id, a workflow node — and is
	// unique per workspace, never globally: a key is client-chosen, and one
	// tenant must not be able to collide with another's by guessing it.
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

	// ChargedMicros is what we actually took, recorded because refunds re-price
	// at today's prices and would otherwise silently disagree with the debit.
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

// SendAttemptStatus is a one-way street. Nothing returns a row to pending, which
// is what makes "the money gate is crossed at most once" a property of the
// database rather than a property of how carefully the code is written.
type SendAttemptStatus string

const (
	// SendAttemptPending: the row won its insert. Nothing has been spent.
	SendAttemptPending SendAttemptStatus = "pending"
	// SendAttemptCharged: the debit committed. From here the provider call is
	// allowed to happen, and only from here.
	SendAttemptCharged SendAttemptStatus = "charged"
	// SendAttemptSent: the provider accepted it and told us the message id.
	SendAttemptSent SendAttemptStatus = "sent"
	// SendAttemptRejected: the provider refused it. Nothing was delivered, so the
	// charge must come back.
	SendAttemptRejected SendAttemptStatus = "rejected"
	// SendAttemptUnknown: we do not know, and guessing costs money in both
	// directions. Left for reconciliation rather than resolved optimistically.
	SendAttemptUnknown SendAttemptStatus = "unknown"
	// SendAttemptRefunded: the charge has been credited back, once.
	SendAttemptRefunded SendAttemptStatus = "refunded"
)

// legalTransitions is the whole state machine, written as data so the repository
// can turn each edge into a WHERE clause and let Postgres arbitrate.
var legalTransitions = map[SendAttemptStatus][]SendAttemptStatus{
	SendAttemptPending: {SendAttemptCharged},
	// charged → refunded is the RECONCILE SWEEP's whole job: money left, no send
	// was ever confirmed, and after the TTL it comes back. Omitting this edge did
	// not prevent those refunds — the sweep credited the customer and then failed
	// to record it, leaving the row `charged` with an unchanged updated_at so it
	// was re-listed every hour forever. Sorted oldest-first, those zombies
	// eventually filled the whole batch and the backstop silently stopped
	// refunding anything newer.
	SendAttemptCharged: {SendAttemptSent, SendAttemptRejected, SendAttemptUnknown, SendAttemptRefunded},
	// sent → refunded is delivery-failure settlement, and it is legal because
	// "accepted" and "delivered" are different facts. Meta bills on DELIVERY, so
	// a failed delivery status after an accepted send means we owe the money back.
	// The earlier reading — "a delivered message is never refunded" — conflated
	// the two and contradicted the webhook path, which refunds exactly this.
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

// PredecessorsOf is what a guarded UPDATE puts in its WHERE clause, so a stale
// writer loses instead of overwriting.
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

// IsTerminal reports whether the attempt is finished and a replay may simply be
// answered from the row instead of doing anything.
func (s SendAttemptStatus) IsTerminal() bool {
	return s == SendAttemptSent || s == SendAttemptRefunded
}

// InFlight is the state a replay must refuse rather than resolve: money has left
// and we do not yet know whether a message did.
func (s SendAttemptStatus) InFlight() bool {
	return s == SendAttemptPending || s == SendAttemptCharged
}

// NeedsReconciliation reports whether the sweep should look at this row once it
// has aged past the reconcile window.
func (a *SendAttempt) NeedsReconciliation(now time.Time, ttl time.Duration) bool {
	if a == nil {
		return false
	}
	if a.Status != SendAttemptCharged && a.Status != SendAttemptUnknown {
		return false
	}
	return now.Sub(a.UpdatedAt) >= ttl
}

// SendOutcome is what the provider did, as opposed to what our HTTP call
// returned. The two are not the same, and conflating them is the single most
// expensive mistake available here.
type SendOutcome string

const (
	// OutcomeAccepted: a message id came back. Ordinary success.
	OutcomeAccepted SendOutcome = "accepted"
	// OutcomeAcceptedNoID: the provider took it and we could not read the answer.
	// Delivered, as far as the customer is concerned. NEVER refund this.
	OutcomeAcceptedNoID SendOutcome = "accepted_no_id"
	// OutcomeRejected: the provider refused it. Nothing was delivered — refund.
	OutcomeRejected SendOutcome = "rejected"
	// OutcomeUnknown: the request may or may not have arrived. Refunding risks
	// crediting a delivered message; not refunding risks holding money for a
	// message that never left. Reconciliation decides, not us.
	OutcomeUnknown SendOutcome = "unknown"
)

// ClassifySendOutcome turns a provider call into a billing decision.
//
// Pure on purpose: this is the function that decides whether money moves back,
// and it must be testable without a network, a database or a clock.
//
// The distinction it draws is between an HTTP status the provider CHOSE — a 4xx
// is a considered refusal, nothing was sent — and everything else, where the
// truth is unobservable. A 5xx or a dead socket is not evidence that the message
// failed; it is the absence of evidence either way.
func ClassifySendOutcome(out *conversation.SendTextMessageOutput, err error) SendOutcome {
	if err == nil {
		if out != nil && strings.TrimSpace(out.MessageID) != "" {
			return OutcomeAccepted
		}
		// A success with no id is still a success: the provider answered 2xx and
		// simply told us nothing useful.
		return OutcomeAcceptedNoID
	}

	// The provider accepted it and we failed to parse the reply.
	if errors.Is(err, conversation.ErrSendOutcomeUnknown) {
		return OutcomeAcceptedNoID
	}

	if out != nil && out.ResponseStatus >= 400 && out.ResponseStatus < 500 {
		return OutcomeRejected
	}
	if out != nil && out.ResponseStatus >= 200 && out.ResponseStatus < 300 {
		// 2xx plus an error means we broke, not them.
		return OutcomeAcceptedNoID
	}
	return OutcomeUnknown
}

// ShouldRefund is the one-line answer the callers actually want, kept next to
// the classification so the two cannot drift.
func (o SendOutcome) ShouldRefund() bool { return o == OutcomeRejected }

// StatusFor maps an outcome onto the attempt state machine.
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

// ChargeReferenceID is the string written into the balance ledger for this
// attempt.
//
// The prefix is load-bearing. Department and campaign-type reporting attributes
// a charge by matching reference_id against a campaign id, so an unprefixed uuid
// could in principle collide with one; "waba:" provably cannot, which keeps an
// ad-hoc send from ever being counted as somebody's campaign.
func ChargeReferenceID(attemptID string) string { return "waba:" + attemptID }

// RefundReferenceID mirrors the credit side, matching the shape the ledger
// already uses for campaign refunds ("refund:" + whatever was debited).
func RefundReferenceID(attemptID string) string { return "refund:" + ChargeReferenceID(attemptID) }

// ParseProviderError pulls Meta's own error out of a failed send.
//
// Worth doing rather than reporting the transport error: "this template is
// paused" tells an operator what to fix, while "unexpected status 400" tells
// them to open a support ticket. Truncated because the message is stored on a
// row and shown in a list, and Meta's are occasionally enormous.
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
