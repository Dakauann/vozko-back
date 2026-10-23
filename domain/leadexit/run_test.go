package leadexit

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/workflow"
)

func TestOnlyAFailedRunIsTriedAgain(t *testing.T) {
	cases := map[Outcome]bool{
		OutcomeChanged:   true,
		OutcomeUnchanged: true,
		OutcomeRefused:   true,
		OutcomeFailed:    false,
		OutcomeRunning:   false,
	}
	for outcome, want := range cases {
		if got := outcome.Final(); got != want {
			t.Errorf("%s: got %v, want %v", outcome, got, want)
		}
	}
}

func TestAnOptOutRunSkipsTheAgentTurnEvenWhenNothingChanged(t *testing.T) {
	cases := []struct {
		run  Run
		want bool
	}{
		{Run{Kind: buttonaction.KindOptOut, Outcome: OutcomeChanged}, true},
		{Run{Kind: buttonaction.KindOptOut, Outcome: OutcomeUnchanged}, true},
		{Run{Kind: buttonaction.KindOptOut, Outcome: OutcomeFailed}, false},
		{Run{Kind: buttonaction.KindOptOut, Outcome: OutcomeRefused}, false},
		{Run{Kind: buttonaction.KindHandOff, Outcome: OutcomeChanged}, false},
	}
	for _, tc := range cases {
		if got := tc.run.OptedOut(); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.run, got, tc.want)
		}
	}
}

func TestPermanentRefusalsAreToldApartFromFailures(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		refused bool
		code    string
	}{
		{"workflow gone", workflow.ErrWorkflowNotFound, true, "workflow_not_found"},
		{"workflow paused", fmt.Errorf("start: %w", workflow.ErrWorkflowNotActive), true, "workflow_not_active"},
		{"voice workflow", workflow.ErrInvalidWorkflowType, true, "workflow_type_invalid"},
		{"workflow without trigger", workflow.ErrGraphNoTrigger, true, "workflow_no_trigger"},
		{"workflow without a message trigger", fmt.Errorf("start: %w", workflow.ErrWorkflowNeedsMessageTrigger), true, workflow.NeedsMessageTriggerCode},
		{"workflow capacity", workflow.ErrWorkspaceAtCapacity, false, "workflow_workspace_at_capacity"},
		{"lead not callable", fmt.Errorf("add: %w", calllist.AdmissionRefused(calllist.SkipNoNumber)), true, "call_list_lead_not_admitted"},
		{"call list gone", calllist.ErrListNotFound, true, "call_list_not_found"},
		{"call list archived", calllist.ErrListNotActive, true, "call_list_not_active"},
		{"call list full", calllist.ErrListFull, true, "call_list_full"},
		{"call list still building", calllist.ErrListBuilding, false, "call_list_building"},
		{"lead gone", fmt.Errorf("load: %w", lead.ErrLeadNotFound), true, "lead_not_found"},
		{"consent rule", lead.ErrConsentNotAllowed, true, "lead_consent_not_allowed"},
		{"department out of scope", ia.ErrDepartmentOutOfScope, true, "hand_off_department_out_of_scope"},
		{"member without access", ia.ErrHandOffTargetNoAccess, true, "hand_off_target_no_access"},
		{"button action params", buttonaction.ErrParamsInvalid, true, "button_action_params_invalid"},
		{"database down", errors.New("connection refused"), false, "lead_exit_action_failed"},
	}
	for _, tc := range cases {
		if got := IsRefusal(tc.err); got != tc.refused {
			t.Errorf("%s: refused = %v, want %v", tc.name, got, tc.refused)
		}
		if got := ReasonCode(tc.err); got != tc.code {
			t.Errorf("%s: code = %q, want %q", tc.name, got, tc.code)
		}
	}
	if ReasonCode(nil) != "" {
		t.Fatal("nil error has a code")
	}
}

func TestTheConversationLineCarriesWhatHappenedWithoutTheLeadsText(t *testing.T) {
	d := Decision{Trigger: TriggerButton, Kind: buttonaction.KindHandOff, Action: handOffAt(0, ""), Template: promoRef}
	run := Run{InboundMessageID: "wamid.in", TemplateMessageID: "wamid.tmpl", LeadID: "lead-1", Trigger: TriggerButton, Kind: buttonaction.KindHandOff,
		Outcome: OutcomeChanged, AssignedUserID: "user-9", Confirmation: ConfirmationNone}
	got := run.Details(d)
	want := map[string]string{
		"trigger": "button", "kind": "hand_off", "outcome": "changed", "button_text": "Falar com atendente", "button_index": "0",
		"template_name": "promo_outubro", "template_language": "pt_BR", "department_id": "dep-1", "assigned_user_id": "user-9",
		"inbound_message_id": "wamid.in", "template_message_id": "wamid.tmpl", "lead_id": "lead-1", "confirmation": "none",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["reason"]; ok {
		t.Errorf("a changed run has no reason: %+v", got)
	}

	word := Decision{Trigger: TriggerTemplateWord, Kind: buttonaction.KindOptOut, Word: "sair"}
	failed := Run{Trigger: TriggerTemplateWord, Kind: buttonaction.KindOptOut, Outcome: OutcomeFailed, Reason: "lead_exit_action_failed"}.Details(word)
	if failed["word"] != "sair" || failed["reason"] != "lead_exit_action_failed" || failed["outcome"] != "failed" {
		t.Fatalf("failed = %+v", failed)
	}

	campaignWord := Decision{Trigger: TriggerCampaignWord, Kind: buttonaction.KindOptOut, Word: "sair", CampaignID: "camp-u"}
	left := Run{Trigger: TriggerCampaignWord, Kind: buttonaction.KindOptOut, Outcome: OutcomeChanged}.Details(campaignWord)
	if left["trigger"] != "campaign_word" || left["word"] != "sair" || left["campaign_id"] != "camp-u" {
		t.Fatalf("campaign word = %+v", left)
	}
	if _, ok := failed["campaign_id"]; ok {
		t.Fatalf("a template word names no campaign: %+v", failed)
	}
}
