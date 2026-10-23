package workflow

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/tracepayload"
)

var ErrWebhookInactive = fmt.Errorf("%w: the webhook is switched off", ErrWebhookNotFound)

type WebhookOutcome string

const (
	WebhookOutcomeAccepted       WebhookOutcome = "accepted"
	WebhookOutcomeDuplicate      WebhookOutcome = "duplicate"
	WebhookOutcomeAlreadyRunning WebhookOutcome = "already_running"
	WebhookOutcomeRejected       WebhookOutcome = "rejected"
	WebhookOutcomeIgnored        WebhookOutcome = "ignored"
)

const (
	WebhookEntryByID        = "entry_id"
	WebhookEntryByPhone     = "phone"
	WebhookEntryByLeadPhone = "lead_phone"
	WebhookEntryByLeadEmail = "lead_email"
)

const WebhookReasonInternal = "internal_error"

const (
	MaxPayloadKeys      = 100
	MaxPayloadKeyLength = 64
)

type WebhookDelivery struct {
	ID              string         `json:"id"`
	WebhookID       string         `json:"webhookId"`
	WorkflowID      string         `json:"workflowId"`
	WorkspaceID     string         `json:"workspaceId"`
	ReceivedAt      time.Time      `json:"receivedAt"`
	Outcome         WebhookOutcome `json:"outcome"`
	Reason          string         `json:"reason,omitempty"`
	HTTPStatus      int            `json:"httpStatus"`
	EntryResolution string         `json:"entryResolution,omitempty"`
	EntryID         string         `json:"entryId,omitempty"`
	EntryType       string         `json:"entryType,omitempty"`
	RunID           string         `json:"runId,omitempty"`
	LatencyMs       int64          `json:"latencyMs"`
	PayloadKeys     []string       `json:"payloadKeys,omitempty"`
}

type WebhookDeliveryRepository interface {
	Record(delivery *WebhookDelivery) error
	ListByWorkflow(workspaceID, workflowID string, visibility conversation.VisibilityScope, pagination shared.Pagination) (*shared.PaginatedResult[*WebhookDelivery], error)
	DeleteReceivedBefore(cutoff time.Time, limit int) (int64, error)
}

type webhookRejection struct {
	err    error
	reason string
}

var webhookRejections = []webhookRejection{
	{ErrWebhookInactive, "webhook_inactive"},
	{ErrWebhookNotFound, "webhook_not_found"},
	{ErrWebhookMethodNotAllowed, "method_not_allowed"},
	{ErrWebhookUnauthorized, "unauthorized"},
	{ErrWebhookEntryRequired, "entry_required"},
	{ErrWebhookEntryNotFound, "entry_not_found"},
	{ErrWebhookEntryForbidden, "entry_forbidden"},
	{ErrWebhookLeadContactRequired, "lead_contact_required"},
	{ErrWebhookLeadPhoneInvalid, "lead_phone_invalid"},
	{ErrWebhookLeadNotFound, "lead_not_found"},
	{ErrWebhookTriggerInvalid, "trigger_invalid"},
	{ErrWebhookNoTriggerNode, "no_trigger_node"},
	{ErrWorkflowNotActive, WorkflowNotActiveCode},
	{ErrWorkspaceAtCapacity, "workspace_at_capacity"},
}

func WebhookRejectionReason(err error) string {
	for _, r := range webhookRejections {
		if errors.Is(err, r.err) {
			return r.reason
		}
	}
	return WebhookReasonInternal
}

func PayloadKeys(body map[string]interface{}) []string {
	if len(body) == 0 {
		return nil
	}
	keys := make([]string, 0, len(body))
	for k := range body {
		if len(k) > MaxPayloadKeyLength {
			k, _ = tracepayload.Text(k, MaxPayloadKeyLength)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > MaxPayloadKeys {
		keys = keys[:MaxPayloadKeys]
	}
	return keys
}
