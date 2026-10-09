package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type knownLeadRepo struct {
	byID     *lead_domain.Lead
	byNumber *lead_domain.Lead
	err      error
	numbers  []string
}

func (r *knownLeadRepo) FindByID(workspaceID, id string) (*lead_domain.Lead, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.byID == nil {
		return nil, lead_domain.ErrLeadNotFound
	}
	return r.byID, nil
}

func (r *knownLeadRepo) FindByNumber(workspaceID, number string) (*lead_domain.Lead, error) {
	r.numbers = append(r.numbers, number)
	if r.err != nil {
		return nil, r.err
	}
	if r.byNumber == nil {
		return nil, lead_domain.ErrLeadNotFound
	}
	return r.byNumber, nil
}

func optedOutLead() *lead_domain.Lead {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	return &lead_domain.Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511999999999", OptedOutAt: &at, OptOutSource: lead_domain.OptOutLeadRequest}
}

func blockedLead() *lead_domain.Lead {
	return &lead_domain.Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511999999999", Blocked: true}
}

func anotherOptedOutLeadOnTheNumber() *lead_domain.Lead {
	optedOut := optedOutLead()
	optedOut.ID = "lead-2"
	optedOut.Number = "5511888888888"
	return optedOut
}

func reachableLead() *lead_domain.Lead {
	return &lead_domain.Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511999999999"}
}

func senderWithLeads(leads *knownLeadRepo, sends *recordingTemplateSends, history *recordingHistory) *whatsappSender {
	sender := templateSender(sends, history)
	sender.deps.LeadRepo = leads
	sender.deps.BusinessPhoneRepo = phonesOwnedBy{"phone-1": "ws-1"}
	return sender
}

func phoneRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{ID: "run-2", EntryID: "web-1", EntryType: string(shared.EntryTypeWebchat), WorkspaceID: "ws-1"}
}

func stateWith(values map[string]string) *workflow.RunState {
	state := workflow.NewRunState()
	for key, value := range values {
		state.Set(key, value)
	}
	return &state
}

func TestAWorkflowTemplateIsNotSentToALeadWhoCannotReceiveIt(t *testing.T) {
	cases := []struct {
		name  string
		run   *workflow.WorkflowRun
		state *workflow.RunState
		leads *knownLeadRepo
	}{
		{name: "a conversation whose lead opted out", run: waMediaRun(), leads: &knownLeadRepo{byID: optedOutLead()}},
		{name: "a conversation whose lead is blocked", run: waMediaRun(), leads: &knownLeadRepo{byID: blockedLead()}},
		{name: "a lead named by the run that opted out", run: phoneRun(), state: stateWith(map[string]string{"lead_id": "lead-1", "phone_number": "5511999999999"}), leads: &knownLeadRepo{byID: optedOutLead()}},
		{name: "a lead named by the run that is not in the workspace", run: phoneRun(), state: stateWith(map[string]string{"lead_id": "lead-9", "phone_number": "5511999999999"}), leads: &knownLeadRepo{}},
		{name: "a number whose lead opted out", run: phoneRun(), state: stateWith(map[string]string{"phone_number": "5511999999999"}), leads: &knownLeadRepo{byNumber: optedOutLead()}},
		{name: "a reachable lead named by the run while the number belongs to an opted-out lead", run: phoneRun(), state: stateWith(map[string]string{"lead_id": "lead-1", "phone_number": "5511888888888"}), leads: &knownLeadRepo{byID: reachableLead(), byNumber: anotherOptedOutLeadOnTheNumber()}},
		{name: "a number in a format no lead can hold", run: phoneRun(), state: stateWith(map[string]string{"phone_number": "abc"}), leads: &knownLeadRepo{err: lead_domain.ErrLeadInvalid}},
		{name: "a number whose lead cannot be read", run: phoneRun(), state: stateWith(map[string]string{"phone_number": "5511999999999"}), leads: &knownLeadRepo{err: errors.New("db down")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sends := &recordingTemplateSends{}
			history := &recordingHistory{}

			_, _, err := senderWithLeads(tc.leads, sends, history).SendTemplate(context.Background(), tc.run, "t1", "phone-1", nil, tc.state)

			if err == nil || len(sends.sends) != 0 || len(history.records) != 0 {
				t.Fatalf("err %v sends %d history %d; the template must be refused before it is charged", err, len(sends.sends), len(history.records))
			}
		})
	}
}

func TestAnOptedOutRefusalIsNamedForTheRunLog(t *testing.T) {
	_, _, err := senderWithLeads(&knownLeadRepo{byID: optedOutLead()}, &recordingTemplateSends{}, &recordingHistory{}).
		SendTemplate(context.Background(), waMediaRun(), "t1", "", nil, nil)

	if !errors.Is(err, errTemplateRecipientRefused) {
		t.Fatalf("err %v, want errTemplateRecipientRefused", err)
	}
}

func TestAWorkflowTemplateReachesANumberWithoutALeadOrAReachableLead(t *testing.T) {
	cases := []struct {
		name  string
		run   *workflow.WorkflowRun
		state *workflow.RunState
		leads *knownLeadRepo
	}{
		{name: "a conversation whose lead can receive it", run: waMediaRun(), leads: &knownLeadRepo{byID: reachableLead()}},
		{name: "a number no lead holds", run: phoneRun(), state: stateWith(map[string]string{"phone_number": "5511999999999"}), leads: &knownLeadRepo{}},
		{name: "a number whose lead can receive it", run: phoneRun(), state: stateWith(map[string]string{"phone_number": "5511999999999"}), leads: &knownLeadRepo{byNumber: reachableLead()}},
		{name: "a reachable lead named by the run on a number no lead holds", run: phoneRun(), state: stateWith(map[string]string{"lead_id": "lead-1", "phone_number": "5511777777777"}), leads: &knownLeadRepo{byID: reachableLead()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sends := &recordingTemplateSends{}

			_, _, err := senderWithLeads(tc.leads, sends, &recordingHistory{}).SendTemplate(context.Background(), tc.run, "t1", "phone-1", nil, tc.state)

			if err != nil || len(sends.sends) != 1 {
				t.Fatalf("err %v sends %d, want one billed send", err, len(sends.sends))
			}
		})
	}
}
