package workflow_usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"

	lead_domain "vozko/domain/lead"
	"vozko/domain/workflow"
)

type fakeWebhookLeads struct {
	byPhone map[string]*lead_domain.Lead
	byEmail map[string]*lead_domain.Lead
	created []string
	err     error
}

func newFakeWebhookLeads() *fakeWebhookLeads {
	return &fakeWebhookLeads{byPhone: map[string]*lead_domain.Lead{}, byEmail: map[string]*lead_domain.Lead{}}
}

func (f *fakeWebhookLeads) EnsureLead(_ context.Context, workspaceID, phone, name string) (*lead_domain.Lead, error) {
	if f.err != nil {
		return nil, f.err
	}
	if found, ok := f.byPhone[phone]; ok {
		return found, nil
	}
	created := &lead_domain.Lead{ID: "lead-" + phone, WorkspaceID: workspaceID, Number: phone, Name: name}
	f.byPhone[phone] = created
	f.created = append(f.created, phone)
	return created, nil
}

func (f *fakeWebhookLeads) FindByEmail(_ string, email string) (*lead_domain.Lead, error) {
	if found, ok := f.byEmail[email]; ok {
		return found, nil
	}
	return nil, lead_domain.ErrLeadNotFound
}

func leadWebhookHarness(t *testing.T, config map[string]interface{}) *webhookHarness {
	t.Helper()
	h := newWebhookHarness()
	h.wfRepo.workflows["wf1"].Graph.Nodes[0].Config = config
	return h
}

func leadReq(body string) WebhookRequest {
	return WebhookRequest{Token: "tok", Method: "POST", RawBody: []byte(body), Header: headerFunc(nil)}
}

func TestALeadWebhookFindsOrCreatesTheLeadByPhone(t *testing.T) {
	h := leadWebhookHarness(t, map[string]interface{}{workflow.WebhookStartOnKey: "lead"})
	res, err := h.uc.Execute(leadReq(`{"event":"abandoned_cart","external_id":"cart-9","phone":"+55 11 99999-0000","name":"Ana","cart_url":"https://loja.example/c/9"}`))
	if err != nil || res.RunID == "" || res.Duplicate {
		t.Fatalf("result = %+v err = %v", res, err)
	}
	if len(h.leads.created) != 1 {
		t.Fatalf("created = %v", h.leads.created)
	}
	run := h.launcher.launched[0]
	if run.EntryType != workflow.EntryTypeLead || run.EntryID != h.leads.byPhone[h.leads.created[0]].ID || run.TriggerType != workflow.TriggerWebhook {
		t.Fatalf("run = %+v", run)
	}
	if run.State.Vars[workflow.DataKeyLeadName] != "Ana" || run.State.Vars["webhook"] == nil {
		t.Fatalf("vars = %+v", run.State.Vars)
	}
	d := h.deliveries.only(t)
	if d.Outcome != workflow.WebhookOutcomeAccepted || d.EntryResolution != workflow.WebhookEntryByLeadPhone || d.EntryType != workflow.EntryTypeLead || d.EntryID != run.EntryID {
		t.Fatalf("delivery = %+v", d)
	}
}

func TestALeadWebhookStartsOncePerStoreEvent(t *testing.T) {
	h := leadWebhookHarness(t, map[string]interface{}{workflow.WebhookStartOnKey: "lead"})
	first, err := h.uc.Execute(leadReq(`{"event":"order_paid","external_id":"o-1","phone":"5511999990000"}`))
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.uc.Execute(leadReq(`{"event":"order_paid","external_id":"o-1","phone":"5511999990000","resent":true}`))
	if err != nil || !again.Duplicate || again.RunID != first.RunID {
		t.Fatalf("again = %+v err = %v, the same store event starts one run", again, err)
	}
	other, err := h.uc.Execute(leadReq(`{"event":"order_paid","external_id":"o-2","phone":"5511999990000"}`))
	if err != nil || other.Duplicate || other.RunID == first.RunID {
		t.Fatalf("another order starts its own run: %+v err = %v", other, err)
	}
}

func TestALeadWebhookFindsAnExistingLeadByEmail(t *testing.T) {
	h := leadWebhookHarness(t, map[string]interface{}{workflow.WebhookStartOnKey: "lead"})
	h.leads.byEmail["ana@example.com"] = &lead_domain.Lead{ID: "lead-mail", WorkspaceID: "ws1", Name: "Ana"}
	if _, err := h.uc.Execute(leadReq(`{"email":"ana@example.com"}`)); err != nil {
		t.Fatal(err)
	}
	if run := h.launcher.launched[0]; run.EntryID != "lead-mail" || run.EntryType != workflow.EntryTypeLead {
		t.Fatalf("run = %+v", run)
	}
	if d := h.deliveries.only(t); d.EntryResolution != workflow.WebhookEntryByLeadEmail {
		t.Fatalf("delivery = %+v", d)
	}
}

func TestALeadWebhookRefusesWhatCannotNameALead(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		err    error
		reason string
		status int
	}{
		{"no phone or email", `{"event":"abandoned_cart"}`, workflow.ErrWebhookLeadContactRequired, "lead_contact_required", http.StatusBadRequest},
		{"invalid phone", `{"phone":"abc"}`, workflow.ErrWebhookLeadPhoneInvalid, "lead_phone_invalid", http.StatusUnprocessableEntity},
		{"unknown email", `{"email":"nobody@example.com"}`, workflow.ErrWebhookLeadNotFound, "lead_not_found", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := leadWebhookHarness(t, map[string]interface{}{workflow.WebhookStartOnKey: "lead"})
			if _, err := h.uc.Execute(leadReq(tc.body)); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			d := h.deliveries.only(t)
			if d.Outcome != workflow.WebhookOutcomeRejected || d.Reason != tc.reason || d.HTTPStatus != tc.status || len(h.launcher.launched) != 0 {
				t.Fatalf("delivery = %+v", d)
			}
		})
	}
}

func TestAWebhookIgnoresEventsItDoesNotListen(t *testing.T) {
	for _, start := range []string{"conversation", "lead"} {
		t.Run(start, func(t *testing.T) {
			h := leadWebhookHarness(t, map[string]interface{}{workflow.WebhookStartOnKey: start, workflow.WebhookEventsKey: []interface{}{"abandoned_cart"}})
			res, err := h.uc.Execute(leadReq(`{"event":"order_paid","entry_id":"entry1","entry_type":"whatsapp","phone":"5511999990000"}`))
			if err != nil || !res.Ignored || res.RunID != "" || len(h.launcher.launched) != 0 || len(h.leads.created) != 0 {
				t.Fatalf("result = %+v err = %v", res, err)
			}
			d := h.deliveries.only(t)
			if d.Outcome != workflow.WebhookOutcomeIgnored || d.HTTPStatus != http.StatusAccepted {
				t.Fatalf("delivery = %+v", d)
			}
		})
	}
}

func TestAWebhookWithUnreadableSettingsStartsNothing(t *testing.T) {
	h := leadWebhookHarness(t, map[string]interface{}{workflow.WebhookStartOnKey: "contact"})
	if _, err := h.uc.Execute(leadReq(`{"phone":"5511999990000"}`)); !errors.Is(err, workflow.ErrWebhookTriggerInvalid) {
		t.Fatalf("err = %v", err)
	}
	if d := h.deliveries.only(t); d.Reason != "trigger_invalid" || d.HTTPStatus != http.StatusUnprocessableEntity || len(h.launcher.launched) != 0 {
		t.Fatalf("delivery = %+v", d)
	}
}
