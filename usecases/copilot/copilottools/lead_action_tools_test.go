package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	"vozko/domain/selection"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	leadaction_usecase "vozko/usecases/leadaction"
	leadsend_usecase "vozko/usecases/leadsend"
)

const (
	firstPart  = "8e2d4c6a-1b3f-4d5e-9a7b-0c1d2e3f4a5b"
	secondPart = "9f3e5d7b-2c4a-4e6f-8b9c-1d2e3f4a5b6c"
	somePhone  = "2a4c6e8f-1b3d-4f5a-9c7e-0d2f4a6b8c1e"
	someTmpl   = "3b5d7f9a-2c4e-4a6b-8d0f-1e3a5c7e9b2d"
)

type fakeLeadActions struct {
	previews   []leadaction_usecase.Request
	starts     []leadaction_usecase.Request
	preview    *leadaction.Preview
	outcome    leadaction_usecase.Outcome
	previewErr error
	startErr   error
	status     *leadaction.Preview
	statusErr  error
	statuses   []string
}

func (f *fakeLeadActions) Preview(_ context.Context, req leadaction_usecase.Request) (*leadaction.Preview, error) {
	f.previews = append(f.previews, req)
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	p := *f.preview
	p.Action = req.Action
	return &p, nil
}

func (f *fakeLeadActions) PreviewStatus(_ context.Context, a leadaction_usecase.Actor, id string) (*leadaction.Preview, error) {
	f.statuses = append(f.statuses, a.WorkspaceID+"/"+id)
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.status == nil {
		return nil, leadaction.ErrPreviewNotFound
	}
	return f.status, nil
}

func (f *fakeLeadActions) Start(_ context.Context, req leadaction_usecase.Request) (leadaction_usecase.Outcome, error) {
	f.starts = append(f.starts, req)
	return f.outcome, f.startErr
}

type fakeLeadSends struct {
	reviews  []leadsend_usecase.SendRequest
	starts   []leadsend_usecase.SendRequest
	cancels  []leadsend_usecase.SendRequest
	review   *campaign.SendReview
	err      error
	startErr error
}

func (f *fakeLeadSends) Review(_ context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error) {
	f.reviews = append(f.reviews, req)
	return f.review, f.err
}

func (f *fakeLeadSends) Start(_ context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error) {
	f.starts = append(f.starts, req)
	if f.startErr != nil {
		return nil, f.startErr
	}
	started := *f.review
	started.Started = true
	return &started, nil
}

func (f *fakeLeadSends) Cancel(_ context.Context, req leadsend_usecase.SendRequest) error {
	f.cancels = append(f.cancels, req)
	return f.err
}

type fakeProposals struct {
	values  map[string]string
	ttls    map[string]time.Duration
	err     error
	setErr  error
	deleted []string
}

func newProposals() *fakeProposals {
	return &fakeProposals{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (f *fakeProposals) SetString(key, value string, ttl time.Duration) error {
	if f.err != nil {
		return f.err
	}
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key], f.ttls[key] = value, ttl
	return nil
}

func (f *fakeProposals) GetString(key string) (string, error) {
	return f.values[key], f.err
}

func (f *fakeProposals) Del(keys ...string) error {
	if f.err != nil {
		return f.err
	}
	for _, key := range keys {
		delete(f.values, key)
		f.deleted = append(f.deleted, key)
	}
	return nil
}

func counted(matched, eligible int, skipped map[leadaction.SkipReason]int) *leadaction.Preview {
	return &leadaction.Preview{
		ID: "preview-1", Status: leadaction.PreviewDone,
		Result: leadaction.PreviewResult{Matched: matched, ExpectedCount: matched, Fingerprint: "fp-1", Selected: matched, Eligible: eligible, Skipped: skipped},
	}
}

type actionHarness struct {
	actions   *fakeLeadActions
	sends     *fakeLeadSends
	proposals *fakeProposals
	deps      LeadActionDeps
	at        time.Time
}

func newActionHarness(preview *leadaction.Preview) *actionHarness {
	leads, _ := filterDeps()
	h := &actionHarness{
		actions:   &fakeLeadActions{preview: preview},
		sends:     &fakeLeadSends{},
		proposals: newProposals(),
	}
	h.at = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	h.deps = LeadActionDeps{Leads: leads, Actions: h.actions, Sends: h.sends, Proposals: h.proposals, Now: func() time.Time { return h.at }}
	return h
}

func manager() copilot.Context {
	cc := member()
	selected := "dept-1"
	cc.Departments = &wd.DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &selected}
	cc.ProposalID = "act-1"
	return cc
}

func proposal(id string) copilot.Context {
	cc := manager()
	cc.ProposalID = id
	return cc
}

func approve(t *testing.T, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) copilot.Result {
	t.Helper()
	if err := tool.(copilot.Validator).Validate(context.Background(), cc, args); err != nil {
		t.Fatalf("Validate on approval = %v", err)
	}
	return tool.Execute(context.Background(), cc, args)
}

func propose(t *testing.T, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	t.Helper()
	if err := tool.(copilot.Validator).Validate(context.Background(), cc, args); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), cc, args)
	tool.(copilot.Previewer).Preview(context.Background(), cc, args)
	return fields
}

func TestLeadActionToolsAreProposalsWithEverySafeguard(t *testing.T) {
	h := newActionHarness(counted(1, 1, nil))
	for _, tool := range []copilot.Tool{NewPrepareLeadActionTool(h.deps), NewStartLeadSendTool(h.deps), NewCancelLeadSendTool(h.deps)} {
		name := tool.Definition().Name
		if m := tool.Meta(); !m.Mutating || m.Resource != "leads" || m.Action != "read" {
			t.Fatalf("%s meta = %+v", name, m)
		}
		if _, ok := tool.(copilot.Validator); !ok {
			t.Fatalf("%s has no preflight", name)
		}
		if _, ok := tool.(copilot.Describer); !ok {
			t.Fatalf("%s has no card", name)
		}
		if _, ok := tool.(copilot.Previewer); !ok {
			t.Fatalf("%s shows no preview", name)
		}
		if _, graded := tool.(copilot.Graded); graded {
			t.Fatalf("%s must always ask for approval", name)
		}
	}
}

func TestPrepareLeadActionClassifiesTheLeadsOfAFilterAsShownOnTheCard(t *testing.T) {
	h := newActionHarness(counted(340, 320, map[leadaction.SkipReason]int{leadaction.SkipUnchanged: 19, leadaction.SkipGone: 1}))
	h.actions.outcome = leadaction_usecase.Outcome{Run: &leadaction.Run{ID: "run-1", Action: leadaction.ActionClassify, Status: leadaction.StatusQueued, Result: leadaction.Result{Matched: 340, Selected: 340}}}
	tool := NewPrepareLeadActionTool(h.deps)
	args := map[string]interface{}{"action": "classify", "cidade": "sp:campinas", "field_key": "interesse", "value": "alto"}
	fields := propose(t, tool, manager(), args)

	req := h.actions.previews[0]
	if req.Actor != (conversation.Viewer{UserID: "u-1", WorkspaceID: "ws-1"}) || req.DepartmentID != "dept-1" || req.DepartmentFilter == nil {
		t.Fatalf("request = %+v", req)
	}
	if req.Action != leadaction.ActionClassify || req.Params.Key != "interesse" || string(req.Params.Value) != `"alto"` {
		t.Fatalf("params = %+v", req.Params)
	}
	if req.Selection.Mode != selection.ModeAllMatching || req.Selection.Filter == nil || req.Selection.Filter.Fields()[0] != crmfilter.FieldCity {
		t.Fatalf("selection = %+v", req.Selection)
	}
	if !strings.Contains(fieldValue(fields, "leads"), "340") || !strings.Contains(fieldValue(fields, "changes"), "320") ||
		!strings.Contains(fieldValue(fields, "skipped"), "19") || !strings.Contains(fieldValue(fields, "action"), "interesse") {
		t.Fatalf("card = %+v", fields)
	}
	if len(h.actions.previews) != 1 {
		t.Fatalf("the card must reuse the preview of the preflight, previews = %d", len(h.actions.previews))
	}

	res := tool.Execute(context.Background(), manager(), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	start := h.actions.starts[0]
	if start.Selection.ExpectedCount != 340 || start.Selection.Fingerprint != "fp-1" || start.IdempotencyKey != "elo-act-1" || start.Locale != "pt" {
		t.Fatalf("start = %+v", start)
	}
	b, _ := json.Marshal(res.Data)
	if !strings.Contains(string(b), `"run_id":"run-1"`) {
		t.Fatalf("data = %s", b)
	}
}

func TestPrepareLeadActionStartsOnlyWhatACardShowed(t *testing.T) {
	h := newActionHarness(counted(10, 10, nil))
	res := NewPrepareLeadActionTool(h.deps).Execute(context.Background(), manager(), map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"})
	if res.Status != copilot.StatusError || len(h.actions.starts) != 0 {
		t.Fatalf("result = %+v, starts %d", res, len(h.actions.starts))
	}
}

func TestPrepareLeadActionTellsWhenTheSelectionChangedSinceTheCard(t *testing.T) {
	h := newActionHarness(counted(10, 10, nil))
	h.actions.startErr = &selection.CountChangedError{Expected: 10, Matched: 14}
	tool := NewPrepareLeadActionTool(h.deps)
	args := map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}
	propose(t, tool, manager(), args)
	res := tool.Execute(context.Background(), manager(), args)
	if res.Status != copilot.StatusError || !strings.Contains(res.Message, "mudou") {
		t.Fatalf("result = %+v", res)
	}
}

func TestPrepareLeadActionBuildsEachKindOfSelection(t *testing.T) {
	screen := &crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.FieldState, crmfilter.OpIn, "SP")}}
	cases := []struct {
		name  string
		cc    copilot.Context
		args  map[string]interface{}
		check func(selection.Selection) bool
	}{
		{"picked leads", manager(), map[string]interface{}{"lead_ids": []interface{}{knownLead, knownLead}},
			func(s selection.Selection) bool {
				return s.Mode == selection.ModeIDs && len(s.IDs) == 1 && s.IDs[0] == knownLead && s.Filter == nil
			}},
		{"the whole base", manager(), map[string]interface{}{"everyone": true},
			func(s selection.Selection) bool { return s.Mode == selection.ModeEveryone && s.Filter == nil }},
		{"the first leads of a filter", manager(), map[string]interface{}{"cidade": "sp:campinas", "limit": 500},
			func(s selection.Selection) bool {
				return s.Mode == selection.ModeFirstN && s.Limit == 500 && len(s.Sort) == 1 && s.Sort[0].Field == crmfilter.Field(lead.SortLastActivityAt) && s.Sort[0].Desc
			}},
		{"the screen filter", onLeads(screen), map[string]interface{}{"use_screen_filter": true},
			func(s selection.Selection) bool {
				return s.Mode == selection.ModeAllMatching && s.Filter.Fields()[0] == crmfilter.FieldState
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newActionHarness(counted(1, 1, nil))
			args := map[string]interface{}{"action": "block", "blocked": true}
			for k, v := range tc.args {
				args[k] = v
			}
			cc := tc.cc
			cc.ProposalID = "act-1"
			propose(t, NewPrepareLeadActionTool(h.deps), cc, args)
			if s := h.actions.previews[0].Selection; !tc.check(s) {
				t.Fatalf("selection = %+v", s)
			}
		})
	}
}

func TestPrepareLeadActionBuildsTheParamsOfEachAction(t *testing.T) {
	yes := true
	cases := []struct {
		name  string
		cc    copilot.Context
		args  map[string]interface{}
		check func(leadaction.Params) bool
	}{
		{"assign to the user", memberWithID(someOwner), map[string]interface{}{"action": "assign_owner", "new_owner_id": "me"},
			func(p leadaction.Params) bool { return p.OwnerID != nil && *p.OwnerID == someOwner }},
		{"remove the owner", manager(), map[string]interface{}{"action": "assign_owner", "new_owner_id": "none"},
			func(p leadaction.Params) bool { return p.OwnerID != nil && *p.OwnerID == "" }},
		{"unblock on the WhatsApp number too", manager(), map[string]interface{}{"action": "block", "blocked": false, "business_phone_id": somePhone},
			func(p leadaction.Params) bool {
				return p.Blocked != nil && !*p.Blocked && p.BusinessPhoneID == somePhone
			}},
		{"clear a field", manager(), map[string]interface{}{"action": "classify", "field_key": "interesse", "clear": true},
			func(p leadaction.Params) bool { return p.Key == "interesse" && p.ClearsValue() }},
		{"export with addresses", manager(), map[string]interface{}{"action": "export", "addresses": true},
			func(p leadaction.Params) bool {
				return p.Format == leadaction.ExportFormatCSV && p.Addresses == yes && !p.Sensitive
			}},
		{"an official send", manager(), map[string]interface{}{"action": "send_template", "name": "Convite", "business_phone_id": somePhone, "template_id": someTmpl,
			"department_id": knownDepartment, "variables": []interface{}{map[string]interface{}{"source": "lead.first_name"}, map[string]interface{}{"source": "literal", "value": "sábado"}}},
			func(p leadaction.Params) bool {
				s := p.Send
				return s != nil && s.Name == "Convite" && s.BusinessPhoneID == somePhone && s.TemplateID == someTmpl && s.DepartmentID == knownDepartment &&
					len(s.Bindings) == 2 && s.Bindings[0].Source == campaign.BindFirstName && s.Bindings[1].Value == "sábado" && s.InstanceID == ""
			}},
		{"an unofficial send", manager(), map[string]interface{}{"action": "send_unofficial", "name": "Aviso", "number_id": somePhone, "message": "Oi {{1}}", "daily_cap": 300,
			"variables": []interface{}{map[string]interface{}{"source": "lead.first_name"}}},
			func(p leadaction.Params) bool {
				s := p.Send
				return s != nil && s.InstanceID == somePhone && s.Message != nil && s.Message.Kind == uwc.KindText && s.Message.Bodies[0] == "Oi {{1}}" &&
					s.DailyCap == 300 && s.TemplateID == "" && len(s.Bindings) == 1
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview := counted(5, 5, nil)
			preview.Send = &campaign.SendQuote{Count: 5, Parts: 1, CostMicros: 400000, BalanceMicros: 10000000, Affordable: true, Currency: "USD"}
			h := newActionHarness(preview)
			args := map[string]interface{}{"cidade": "sp:campinas"}
			for k, v := range tc.args {
				args[k] = v
			}
			cc := tc.cc
			cc.ProposalID = "act-1"
			propose(t, NewPrepareLeadActionTool(h.deps), cc, args)
			if p := h.actions.previews[0].Params; !tc.check(p) {
				t.Fatalf("params = %+v (send %+v)", p, p.Send)
			}
		})
	}
}

func TestPrepareLeadActionRefusesBeforeTheCard(t *testing.T) {
	unexpected := errors.New("unexpected")
	cases := []struct {
		name       string
		args       map[string]interface{}
		preview    *leadaction.Preview
		previewErr error
		want       string
	}{
		{"no selection", map[string]interface{}{"action": "block", "blocked": true}, nil, nil, "everyone"},
		{"picked leads and a filter", map[string]interface{}{"action": "block", "blocked": true, "lead_ids": []interface{}{knownLead}, "cidade": "sp:campinas"}, nil, nil, "lead_ids"},
		{"the whole base and a filter", map[string]interface{}{"action": "block", "blocked": true, "everyone": true, "cidade": "sp:campinas"}, nil, nil, "everyone"},
		{"a phone number as the search", map[string]interface{}{"action": "block", "blocked": true, "query": "+55 84 99440-9624"}, nil, nil, "lead_ids"},
		{"a local number as the search", map[string]interface{}{"action": "block", "blocked": true, "query": "98765-4321"}, nil, nil, "lead_ids"},
		{"a bare subscriber number as the search", map[string]interface{}{"action": "block", "blocked": true, "query": "987654321"}, nil, nil, "lead_ids"},
		{"eight digits as the search", map[string]interface{}{"action": "block", "blocked": true, "query": "maria 3456 7890"}, nil, nil, "lead_ids"},
		{"picked leads and a state", map[string]interface{}{"action": "block", "blocked": true, "lead_ids": []interface{}{knownLead}, "uf": "SP"}, nil, nil, "lead_ids"},
		{"the whole base and a state", map[string]interface{}{"action": "block", "blocked": true, "everyone": true, "uf": "SP"}, nil, nil, "everyone"},
		{"outside an approval card", map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}, nil, nil, "cartão"},
		{"a sensitive field", map[string]interface{}{"action": "classify", "cidade": "sp:campinas", "field_key": "posicao", "value": "Positivo"}, nil, nil, "sensível"},
		{"an unknown field", map[string]interface{}{"action": "classify", "cidade": "sp:campinas", "field_key": "salario", "value": "1"}, nil, nil, "interesse"},
		{"a value the field does not take", map[string]interface{}{"action": "classify", "cidade": "sp:campinas", "field_key": "interesse", "value": "médio"}, nil, nil, "interesse"},
		{"block without saying which way", map[string]interface{}{"action": "block", "cidade": "sp:campinas"}, nil, nil, "blocked"},
		{"an unknown action", map[string]interface{}{"action": "delete", "cidade": "sp:campinas"}, nil, nil, "action"},
		{"nobody matches", map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}, counted(0, 0, nil), nil, "nenhum lead"},
		{"a refused preview", map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}, counted(1, 1, nil), campaign.ErrCreationScopeMissing, "nenhum departamento"},
		{"an unexpected failure", map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}, counted(1, 1, nil), unexpected, "falha"},
		{"a screen group without a conjunction", map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}, counted(1, 1, nil), fmt.Errorf("group 0: %w", crmfilter.ErrConjunctionRequired), "grupo"},
		{"a send that needs splitting", map[string]interface{}{"action": "send_template", "name": "x", "business_phone_id": somePhone, "template_id": someTmpl, "everyone": true},
			&leadaction.Preview{Status: leadaction.PreviewDone, Result: leadaction.PreviewResult{Matched: 200000, ExpectedCount: 200000, Selected: 200000}, Send: &campaign.SendQuote{Count: 200000, Parts: 2, SplitRequired: true}}, nil, "split"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview := tc.preview
			if preview == nil {
				preview = counted(3, 3, nil)
			}
			h := newActionHarness(preview)
			h.actions.previewErr = tc.previewErr
			cc := manager()
			if tc.name == "outside an approval card" {
				cc.ProposalID = ""
			}
			err := NewPrepareLeadActionTool(h.deps).(copilot.Validator).Validate(context.Background(), cc, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

func TestPrepareLeadActionPreparesASendAndPointsToItsStart(t *testing.T) {
	preview := counted(1200, 1200, nil)
	preview.Send = &campaign.SendQuote{Count: 1200, Parts: 1, UnitPriceMicros: 8000, CostMicros: 9600000, BalanceMicros: 50000000, Affordable: true, Currency: "USD", Fits: 1200}
	h := newActionHarness(preview)
	h.actions.outcome = leadaction_usecase.Outcome{Send: &campaign.SendReview{
		Channel: campaign.ChannelOfficial, Parts: []campaign.SendPart{{CampaignID: firstPart, Name: "Convite", Status: campaign.StatusStopped, Entries: 1200, Eligible: 1150}},
		Entries: 1200, Eligible: 1150, Skipped: map[campaign.SkipReason]int{campaign.SkipOptedOut: 30, campaign.SkipCooldown: 20},
		Counted: map[campaign.CountedReason]int{campaign.CountedWindowOpen: 40},
		Quote:   campaign.SendQuote{Count: 1150, CostMicros: 9200000, BalanceMicros: 50000000, Affordable: true, Fits: 1150},
	}}
	tool := NewPrepareLeadActionTool(h.deps)
	args := map[string]interface{}{"action": "send_template", "name": "Convite", "business_phone_id": somePhone, "template_id": someTmpl, "cidade": "sp:campinas"}
	fields := propose(t, tool, manager(), args)
	if !strings.Contains(fieldValue(fields, "estimatedCost"), "US$ 9.60") || !strings.Contains(fieldValue(fields, "balance"), "US$ 50.00") {
		t.Fatalf("card = %+v", fields)
	}
	res := tool.Execute(context.Background(), manager(), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	b, _ := json.Marshal(res.Data)
	for _, want := range []string{firstPart, `"channel":"official"`, `"opted_out":30`, `"window_open":40`, "start_lead_send"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("data misses %s: %s", want, b)
		}
	}
}

func TestPrepareLeadActionReturnsTheReportOfAnExport(t *testing.T) {
	h := newActionHarness(counted(80, 80, nil))
	h.actions.outcome = leadaction_usecase.Outcome{Report: &report.Job{ID: "job-1", Status: report.StatusQueued}}
	tool := NewPrepareLeadActionTool(h.deps)
	args := map[string]interface{}{"action": "export", "cidade": "sp:campinas"}
	propose(t, tool, manager(), args)
	res := tool.Execute(context.Background(), manager(), args)
	b, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || !strings.Contains(string(b), `"report_id":"job-1"`) {
		t.Fatalf("result = %+v %s", res, b)
	}
}

func TestPrepareLeadActionRefusesWithoutItsPorts(t *testing.T) {
	args := map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}
	for name, deps := range map[string]LeadActionDeps{
		"no actions":   {Proposals: newProposals()},
		"no proposals": {Actions: &fakeLeadActions{preview: counted(1, 1, nil)}},
	} {
		if err := NewPrepareLeadActionTool(deps).(copilot.Validator).Validate(context.Background(), manager(), args); err == nil {
			t.Fatalf("%s: Validate passed", name)
		}
	}
}

func sendReview() *campaign.SendReview {
	return &campaign.SendReview{
		Channel: campaign.ChannelOfficial,
		Parts: []campaign.SendPart{
			{CampaignID: firstPart, Name: "Convite (1/2)", Status: campaign.StatusStopped, Entries: 150000, Eligible: 149000},
			{CampaignID: secondPart, Name: "Convite (2/2)", Status: campaign.StatusStopped, Entries: 30000, Eligible: 29000},
		},
		Entries: 180000, Eligible: 178000,
		Skipped: map[campaign.SkipReason]int{campaign.SkipBlocked: 1000, campaign.SkipCooldown: 1000},
		Counted: map[campaign.CountedReason]int{campaign.CountedNoConsentRecorded: 5000},
		Quote:   campaign.SendQuote{Count: 178000, Parts: 2, UnitPriceMicros: 8000, CostMicros: 1424000000, BalanceMicros: 2000000000, Currency: "USD", Affordable: true, Fits: 178000},
	}
}

func TestStartLeadSendShowsTheFinalCostAndStartsTheReviewedParts(t *testing.T) {
	h := newActionHarness(counted(1, 1, nil))
	h.sends.review = sendReview()
	tool := NewStartLeadSendTool(h.deps)
	args := map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}}
	fields := propose(t, tool, manager(), args)
	for key, want := range map[string]string{"recipients": "178000", "finalCost": "US$ 1424.00", "balance": "US$ 2000.00", "skipped": "1000", "counted": "5000"} {
		if !strings.Contains(fieldValue(fields, key), want) {
			t.Fatalf("card %s = %q, want %q (%+v)", key, fieldValue(fields, key), want, fields)
		}
	}
	res := tool.Execute(context.Background(), manager(), args)
	if res.Status != copilot.StatusOK || len(h.sends.starts) != 1 {
		t.Fatalf("result = %+v", res)
	}
	start := h.sends.starts[0]
	if start.Channel != campaign.ChannelOfficial || len(start.CampaignIDs) != 2 || start.FirstN != 0 || start.Actor.UserID != "u-1" || start.Departments == nil {
		t.Fatalf("start = %+v", start)
	}
}

func TestStartLeadSendRefusesWhatTheBudgetDoesNotCover(t *testing.T) {
	h := newActionHarness(counted(1, 1, nil))
	review := sendReview()
	review.Quote.Affordable, review.Quote.Fits, review.Quote.Refusal, review.Quote.BalanceMicros = false, 120000, "unaffordable", 8000*120000
	h.sends.review = review
	tool := NewStartLeadSendTool(h.deps)
	err := tool.(copilot.Validator).Validate(context.Background(), manager(), map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}})
	if err == nil || !strings.Contains(err.Error(), "first_n") || !strings.Contains(err.Error(), "120000") {
		t.Fatalf("Validate = %v", err)
	}
	args := map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}, "first_n": 120000}
	fields := propose(t, tool, manager(), args)
	if !strings.Contains(fieldValue(fields, "recipients"), "120000") || !strings.Contains(fieldValue(fields, "finalCost"), "US$ 960.00") {
		t.Fatalf("card = %+v", fields)
	}
	if res := tool.Execute(context.Background(), manager(), args); res.Status != copilot.StatusOK || h.sends.starts[0].FirstN != 120000 {
		t.Fatalf("result = %+v, starts %+v", res, h.sends.starts)
	}
}

func TestStartLeadSendRefusesAStartedOrEmptySend(t *testing.T) {
	for name, edit := range map[string]func(*campaign.SendReview){
		"already started":   func(r *campaign.SendReview) { r.Started = true },
		"nobody to receive": func(r *campaign.SendReview) { r.Eligible = 0 },
	} {
		h := newActionHarness(counted(1, 1, nil))
		review := sendReview()
		edit(review)
		h.sends.review = review
		err := NewStartLeadSendTool(h.deps).(copilot.Validator).Validate(context.Background(), manager(), map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart}})
		if err == nil {
			t.Fatalf("%s: Validate passed", name)
		}
	}
}

func TestCancelLeadSendDiscardsTheStoppedParts(t *testing.T) {
	h := newActionHarness(counted(1, 1, nil))
	h.sends.review = sendReview()
	tool := NewCancelLeadSendTool(h.deps)
	args := map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}}
	fields := propose(t, tool, manager(), args)
	if !strings.Contains(fieldValue(fields, "campaign"), "Convite (1/2)") {
		t.Fatalf("card = %+v", fields)
	}
	if res := tool.Execute(context.Background(), manager(), args); res.Status != copilot.StatusOK || len(h.sends.cancels) != 1 {
		t.Fatalf("result = %+v", res)
	}
}

func blockArgs() map[string]interface{} {
	return map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"}
}

func TestApprovingALeadActionNeverCountsTheSelectionAgain(t *testing.T) {
	h := newActionHarness(counted(340, 340, nil))
	h.actions.outcome = leadaction_usecase.Outcome{Run: &leadaction.Run{ID: "run-1"}}
	tool := NewPrepareLeadActionTool(h.deps)
	propose(t, tool, manager(), blockArgs())
	if len(h.actions.previews) != 1 {
		t.Fatalf("one proposal counted %d times", len(h.actions.previews))
	}
	h.at = h.at.Add(time.Hour)
	if res := approve(t, tool, manager(), blockArgs()); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	if len(h.actions.previews) != 1 || len(h.actions.starts) != 1 {
		t.Fatalf("the approval counted again: %d previews, %d starts", len(h.actions.previews), len(h.actions.starts))
	}
	if strings.Join(h.actions.statuses, ",") != "ws-1/preview-1" {
		t.Fatalf("the approval read the preview status %v", h.actions.statuses)
	}
	if res := tool.Execute(context.Background(), manager(), blockArgs()); res.Status != copilot.StatusError || len(h.actions.starts) != 1 {
		t.Fatalf("one card started twice: %+v", res)
	}
}

func TestAnOlderCardNeverRunsWithANewerCount(t *testing.T) {
	h := newActionHarness(counted(10, 10, nil))
	tool := NewPrepareLeadActionTool(h.deps)
	propose(t, tool, proposal("act-1"), blockArgs())
	h.actions.preview = counted(14, 14, nil)
	h.actions.preview.Result.Fingerprint = "fp-2"
	propose(t, tool, proposal("act-2"), blockArgs())
	if res := tool.Execute(context.Background(), proposal("act-1"), blockArgs()); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	start := h.actions.starts[0]
	if start.Selection.ExpectedCount != 10 || start.Selection.Fingerprint != "fp-1" || start.IdempotencyKey != "elo-act-1" {
		t.Fatalf("the older card ran as %+v", start)
	}
}

func TestACardThatWasNotKeptRefusesItsApproval(t *testing.T) {
	h := newActionHarness(counted(10, 10, nil))
	tool := NewPrepareLeadActionTool(h.deps)
	h.proposals.setErr = errors.New("redis down")
	propose(t, tool, manager(), blockArgs())
	h.proposals.setErr = nil
	if res := tool.Execute(context.Background(), manager(), blockArgs()); res.Status != copilot.StatusError || len(h.actions.starts) != 0 {
		t.Fatalf("result = %+v, starts %d", res, len(h.actions.starts))
	}
}

func TestACardRunsOnlyTheArgumentsItShowed(t *testing.T) {
	h := newActionHarness(counted(10, 10, nil))
	tool := NewPrepareLeadActionTool(h.deps)
	propose(t, tool, manager(), blockArgs())
	other := blockArgs()
	other["blocked"] = false
	if res := tool.Execute(context.Background(), manager(), other); res.Status != copilot.StatusError || len(h.actions.starts) != 0 {
		t.Fatalf("result = %+v, starts %d", res, len(h.actions.starts))
	}
}

func TestApprovalRefusesACountThatFailedInTheBackground(t *testing.T) {
	h := newActionHarness(counted(10, 10, nil))
	tool := NewPrepareLeadActionTool(h.deps)
	propose(t, tool, manager(), blockArgs())
	h.actions.status = &leadaction.Preview{ID: "preview-1", Status: leadaction.PreviewFailed, FailureCode: "internal"}
	if err := tool.(copilot.Validator).Validate(context.Background(), manager(), blockArgs()); err == nil || !strings.Contains(err.Error(), "contagem") {
		t.Fatalf("Validate = %v", err)
	}
	h.actions.status, h.actions.statusErr = nil, errors.New("redis down")
	if err := tool.(copilot.Validator).Validate(context.Background(), manager(), blockArgs()); err == nil {
		t.Fatal("an unreadable count status passed")
	}
}

func TestACardOverAPartialCountShowsTheWholeSelection(t *testing.T) {
	running := &leadaction.Preview{ID: "preview-9", Status: leadaction.PreviewRunning, Result: leadaction.PreviewResult{
		Matched: 188000, ExpectedCount: 188000, Fingerprint: "fp-9", Selected: 15000, Eligible: 14800, Skipped: map[leadaction.SkipReason]int{leadaction.SkipUnchanged: 200},
	}}
	h := newActionHarness(running)
	tool := NewPrepareLeadActionTool(h.deps)
	fields := propose(t, tool, manager(), blockArgs())
	if !strings.Contains(fieldValue(fields, "leads"), "188000") || strings.Contains(fieldValue(fields, "leads"), "15000") ||
		!strings.Contains(fieldValue(fields, "changes"), "parcial") {
		t.Fatalf("card = %+v", fields)
	}
	preview := tool.(copilot.Previewer).Preview(context.Background(), manager(), blockArgs())
	data, ok := preview.Data.(leadActionPreview)
	if !ok || data.PreviewID != "preview-9" || !data.Partial || data.Selected != 188000 || data.Eligible != 14800 {
		t.Fatalf("preview = %+v", preview.Data)
	}
	if res := tool.Execute(context.Background(), manager(), blockArgs()); res.Status != copilot.StatusOK || h.actions.starts[0].Selection.ExpectedCount != 188000 {
		t.Fatalf("result = %+v", res)
	}
}

func TestTheFirstLeadsOfAPartialCountAreCountedByTheirLimit(t *testing.T) {
	running := &leadaction.Preview{ID: "preview-9", Status: leadaction.PreviewRunning, Result: leadaction.PreviewResult{Matched: 188000, ExpectedCount: 60000, Selected: 5000, Eligible: 5000}}
	h := newActionHarness(running)
	args := blockArgs()
	args["limit"] = 60000
	fields := propose(t, NewPrepareLeadActionTool(h.deps), manager(), args)
	if label := fieldValue(fields, "leads"); !strings.Contains(label, "60000") || !strings.Contains(label, "188000") {
		t.Fatalf("leads = %q", label)
	}
}

func TestTheMemoKeepsACopyOfTheCount(t *testing.T) {
	h := newActionHarness(counted(10, 10, map[leadaction.SkipReason]int{leadaction.SkipGone: 1}))
	tool := NewPrepareLeadActionTool(h.deps).(*prepareLeadActionTool)
	_, first, err := tool.counted(context.Background(), manager(), blockArgs())
	if err != nil {
		t.Fatal(err)
	}
	first.Result.Skipped[leadaction.SkipGone] = 99
	_, again, err := tool.counted(context.Background(), manager(), blockArgs())
	if err != nil || again.Result.Skipped[leadaction.SkipGone] != 1 || len(h.actions.previews) != 1 {
		t.Fatalf("the memo shared its count: %+v, %v, %d previews", again.Result, err, len(h.actions.previews))
	}
}

func TestALeadExportFollowsTheLocaleOfTheRequest(t *testing.T) {
	h := newActionHarness(counted(80, 80, nil))
	h.actions.outcome = leadaction_usecase.Outcome{Report: &report.Job{ID: "job-1"}}
	tool := NewPrepareLeadActionTool(h.deps)
	cc := manager()
	cc.Locale = "en"
	args := map[string]interface{}{"action": "export", "cidade": "sp:campinas"}
	propose(t, tool, cc, args)
	if res := tool.Execute(context.Background(), cc, args); res.Status != copilot.StatusOK || h.actions.starts[0].Locale != "en" {
		t.Fatalf("result = %+v, starts %+v", res, h.actions.starts)
	}
}

func TestOneSendProposalReviewsTheSendOnce(t *testing.T) {
	for name, tool := range map[string]func(LeadActionDeps) copilot.Tool{"start": NewStartLeadSendTool, "cancel": NewCancelLeadSendTool} {
		h := newActionHarness(counted(1, 1, nil))
		h.sends.review = sendReview()
		propose(t, tool(h.deps), manager(), map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}})
		if len(h.sends.reviews) != 1 {
			t.Fatalf("%s reviewed the send %d times for one proposal", name, len(h.sends.reviews))
		}
	}
}

func TestStartLeadSendRefusesWhatTheBudgetRuleRefuses(t *testing.T) {
	cases := []struct {
		name string
		edit func(*campaign.SendReview)
		args map[string]interface{}
		want string
	}{
		{"a first_n above the eligible", func(*campaign.SendReview) {}, map[string]interface{}{"first_n": 178001}, "first_n"},
		{"a negative first_n", func(*campaign.SendReview) {}, map[string]interface{}{"first_n": -1}, "first_n"},
		{"a template without a price", func(r *campaign.SendReview) { r.Quote.UnitPriceMicros = 0 }, nil, "preço"},
		{"a balance that does not cover the send", func(r *campaign.SendReview) { r.Quote.BalanceMicros = 8000 * 1000 }, nil, "first_n=1000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newActionHarness(counted(1, 1, nil))
			review := sendReview()
			tc.edit(review)
			h.sends.review = review
			args := map[string]interface{}{"channel": "official", "campaign_ids": []interface{}{firstPart, secondPart}}
			for k, v := range tc.args {
				args[k] = v
			}
			err := NewStartLeadSendTool(h.deps).(copilot.Validator).Validate(context.Background(), manager(), args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}
