package leadexit

import (
	"context"
	"strconv"
	"time"

	"vozko/domain/buttonaction"
)

const RunTimeout = 30 * time.Second

type Outcome string

const (
	OutcomeRunning   Outcome = "running"
	OutcomeChanged   Outcome = "changed"
	OutcomeUnchanged Outcome = "unchanged"
	OutcomeRefused   Outcome = "refused"
	OutcomeFailed    Outcome = "failed"
)

func (o Outcome) Final() bool {
	return o == OutcomeChanged || o == OutcomeUnchanged || o == OutcomeRefused
}

type ConfirmationState string

const (
	ConfirmationNone          ConfirmationState = "none"
	ConfirmationSent          ConfirmationState = "sent"
	ConfirmationOutsideWindow ConfirmationState = "outside_window"
	ConfirmationAlreadySent   ConfirmationState = "already_sent"
	ConfirmationNotSent       ConfirmationState = "not_sent"
)

type Run struct {
	WorkspaceID        string
	InboundMessageID   string
	EntryID            string
	EntryType          string
	LeadID             string
	TemplateMessageID  string
	Trigger            Trigger
	Kind               buttonaction.Kind
	Outcome            Outcome
	Reason             string
	AssignedUserID     string
	ConsentEventID     string
	Confirmation       ConfirmationState
	ConfirmationReason string
	UpdatedAt          time.Time
}

type RunClaim struct {
	Claimed  bool
	Previous Run
}

type RunStore interface {
	Claim(ctx context.Context, run Run) (RunClaim, error)
	Finish(ctx context.Context, run Run) error
}

func Reclaimable(outcome Outcome, claimedAt, now time.Time) bool {
	switch outcome {
	case OutcomeFailed:
		return true
	case OutcomeRunning:
		return !now.Before(claimedAt.Add(RunTimeout))
	}
	return false
}

func (r Run) OptedOut() bool {
	return r.Kind == buttonaction.KindOptOut && (r.Outcome == OutcomeChanged || r.Outcome == OutcomeUnchanged)
}

func (r Run) Details(d Decision) map[string]string {
	details := map[string]string{
		"trigger": string(r.Trigger),
		"kind":    string(r.Kind),
		"outcome": string(r.Outcome),
	}
	put := func(key, value string) {
		if value != "" {
			details[key] = value
		}
	}
	put("reason", r.Reason)
	put("word", d.Word)
	put("template_name", d.Template.Name)
	put("template_language", d.Template.Language)
	put("campaign_id", d.CampaignID)
	put("inbound_message_id", r.InboundMessageID)
	put("template_message_id", r.TemplateMessageID)
	put("lead_id", r.LeadID)
	put("assigned_user_id", r.AssignedUserID)
	put("consent_event_id", r.ConsentEventID)
	put("confirmation", string(r.Confirmation))
	put("confirmation_reason", r.ConfirmationReason)
	if d.Action != nil {
		details["button_index"] = strconv.Itoa(d.Action.ButtonIndex)
		put("button_text", d.Action.ButtonText)
		putParams(put, d.Action.Params)
	}
	return details
}

func putParams(put func(string, string), p buttonaction.Params) {
	put("department_id", p.DepartmentID)
	put("user_id", p.UserID)
	put("workflow_id", p.WorkflowID)
	put("call_list_id", p.CallListID)
	put("field_key", p.FieldKey)
	put("option", p.Option)
	put("pipeline_id", p.PipelineID)
	put("stage_id", p.StageID)
}
