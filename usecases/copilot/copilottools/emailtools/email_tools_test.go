package emailtools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/workspace"
	balance_usecase "vozko/usecases/balance"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
	"vozko/usecases/transactionalemail"
)

const (
	knownDesign   = "6c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f"
	knownCampaign = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	knownLead     = "9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4"
)

type fakeSender struct {
	requests []transactionalemail.Request
	err      error
}

func (f *fakeSender) Send(_ context.Context, req transactionalemail.Request) (transactionalemail.Result, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return transactionalemail.Result{}, f.err
	}
	return transactionalemail.Result{MessageID: "m-1", To: req.To}, nil
}

type fakeCampaigns struct {
	actor    emailcampaign_usecase.Actor
	days     int
	overview emailcampaign_usecase.Overview
	err      error
}

func (f *fakeCampaigns) Overview(_ context.Context, a emailcampaign_usecase.Actor, days int) (emailcampaign_usecase.Overview, error) {
	f.actor, f.days = a, days
	return f.overview, f.err
}

func (f *fakeCampaigns) Metrics(_ context.Context, a emailcampaign_usecase.Actor, campaignID string) (emailcampaign_usecase.CampaignMetrics, error) {
	f.actor = a
	if campaignID != knownCampaign {
		return emailcampaign_usecase.CampaignMetrics{}, emailcampaign.ErrCampaignNotFound
	}
	return emailcampaign_usecase.CampaignMetrics{Status: emailcampaign.StatusCompleted}, nil
}

func (f *fakeCampaigns) Get(_ context.Context, _ emailcampaign_usecase.Actor, campaignID string) (*emailcampaign.Campaign, error) {
	if campaignID != knownCampaign {
		return nil, emailcampaign.ErrCampaignNotFound
	}
	return &emailcampaign.Campaign{ID: knownCampaign, Name: "Black Friday"}, nil
}

func (f *fakeCampaigns) GetDesign(_ context.Context, _ emailcampaign_usecase.Actor, designID string) (*emailcampaign.Design, error) {
	if designID != knownDesign {
		return nil, emailcampaign.ErrDesignNotFound
	}
	return &emailcampaign.Design{ID: knownDesign, Name: "Confirmação"}, nil
}

type fakeQuotes struct{ err error }

func (f fakeQuotes) QuoteRun(context.Context, string, int64) (balance_usecase.EmailQuote, error) {
	return balance_usecase.EmailQuote{Units: 1, UnitMicros: 583, TotalMicros: 583}, f.err
}

func deps() (EmailDeps, *fakeSender, *fakeCampaigns) {
	sender, campaigns := &fakeSender{}, &fakeCampaigns{}
	return EmailDeps{Sender: sender, Campaigns: campaigns, Quotes: fakeQuotes{}}, sender, campaigns
}

var session = copilot.Context{WorkspaceID: "ws-1", UserID: "user-1", ProposalID: "proposal-1"}

func toolNamed(t *testing.T, d EmailDeps, name string) copilot.Tool {
	t.Helper()
	for _, tool := range EmailTools(d) {
		if tool.Definition().Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %s", name)
	return nil
}

func TestEmailToolsPermissions(t *testing.T) {
	d, _, _ := deps()
	want := map[string]copilot.Meta{
		"send_email":             {Mutating: true, Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionStart},
		"email_overview":         {Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionRead},
		"email_campaign_metrics": {Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionRead},
	}
	tools := EmailTools(d)
	if len(tools) != len(want) {
		t.Fatalf("tools = %d", len(tools))
	}
	for _, tool := range tools {
		if got := tool.Meta(); got != want[tool.Definition().Name] {
			t.Errorf("%s meta = %+v", tool.Definition().Name, got)
		}
	}
	send := toolNamed(t, d, "send_email")
	if _, ok := send.(copilot.Validator); !ok {
		t.Fatal("send_email needs a preflight")
	}
	if _, ok := send.(copilot.Describer); !ok {
		t.Fatal("send_email needs a card")
	}
}

func TestSendEmailValidate(t *testing.T) {
	d, sender, _ := deps()
	tool := toolNamed(t, d, "send_email").(copilot.Validator)
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"text", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "text": "Corpo"}, ""},
		{"design and lead", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "design_id": knownDesign, "lead_id": knownLead}, ""},
		{"no to", map[string]interface{}{"subject": "Oi", "text": "Corpo"}, "to"},
		{"bad address", map[string]interface{}{"to": "ana", "subject": "Oi", "text": "Corpo"}, "to"},
		{"no subject", map[string]interface{}{"to": "ana@acme.com.br", "text": "Corpo"}, "subject"},
		{"no body", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi"}, "text"},
		{"both bodies", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "text": "x", "design_id": knownDesign}, "text"},
		{"invented design", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "design_id": "d-1"}, "design_id"},
		{"unknown design", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "design_id": "0f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}, "design"},
		{"subject merge tag", map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi {{nome}}", "text": "x"}, "assunto"},
	}
	for _, tc := range cases {
		err := tool.Validate(context.Background(), session, tc.args)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want mention of %q", tc.name, err, tc.want)
		}
	}
	if len(sender.requests) != 0 {
		t.Fatal("a preflight never sends")
	}
}

func TestSendEmailDescribe(t *testing.T) {
	d, _, _ := deps()
	tool := toolNamed(t, d, "send_email").(copilot.Describer)
	fields := tool.Describe(context.Background(), session, map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "design_id": knownDesign})
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["to"] != "ana@acme.com.br" || got["subject"] != "Oi" || got["design"] != "Confirmação" || got["cost"] != "US$ 0.0006" {
		t.Fatalf("fields = %+v", got)
	}
	text := tool.Describe(context.Background(), session, map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "text": "Corpo do e-mail"})
	found := false
	for _, f := range text {
		found = found || (f.Key == "text" && f.Value == "Corpo do e-mail")
	}
	if !found {
		t.Fatalf("fields = %+v", text)
	}
}

func TestSendEmailExecute(t *testing.T) {
	d, sender, _ := deps()
	tool := toolNamed(t, d, "send_email")
	res := tool.Execute(context.Background(), session, map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "text": "Corpo", "lead_id": knownLead})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	req := sender.requests[0]
	if req.WorkspaceID != "ws-1" || req.To != "ana@acme.com.br" || req.LeadID != knownLead || req.Body.Text != "Corpo" || req.Origin != transactionalemail.OriginCopilot || req.SentBy != conversation.SentByAI("") || req.IdempotencyKey != "copilot:proposal-1" {
		t.Fatalf("request = %+v", req)
	}

	refused, sender2, _ := deps()
	sender2.err = &transactionalemail.Refusal{Code: transactionalemail.CodeRecipientSuppressed, Err: errors.New("suppressed")}
	out := toolNamed(t, refused, "send_email").Execute(context.Background(), session, map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "text": "Corpo"})
	if out.Status != copilot.StatusError || !strings.Contains(out.Message, transactionalemail.CodeRecipientSuppressed) {
		t.Fatalf("refusal = %+v", out)
	}

	noProposal, sender3, _ := deps()
	toolNamed(t, noProposal, "send_email").Execute(context.Background(), copilot.Context{WorkspaceID: "ws-1", UserID: "u"}, map[string]interface{}{"to": "ana@acme.com.br", "subject": "Oi", "text": "Corpo"})
	if key := sender3.requests[0].IdempotencyKey; !strings.HasPrefix(key, "copilot:") || key == "copilot:" {
		t.Fatalf("key = %q", key)
	}
}

func TestEmailOverview(t *testing.T) {
	d, _, campaigns := deps()
	campaigns.overview = emailcampaign_usecase.Overview{Days: 7}
	tool := toolNamed(t, d, "email_overview")
	if res := tool.Execute(context.Background(), session, map[string]interface{}{"days": 7}); res.Status != copilot.StatusOK || campaigns.days != 7 || campaigns.actor.WorkspaceID != "ws-1" || campaigns.actor.UserID != "user-1" {
		t.Fatalf("result = %+v, days %d, actor %+v", res, campaigns.days, campaigns.actor)
	}
	if tool.Execute(context.Background(), session, map[string]interface{}{}); campaigns.days != defaultOverviewDays {
		t.Fatalf("default days = %d", campaigns.days)
	}
	campaigns.err = emailcampaign_usecase.ErrDaysInvalid
	if res := tool.Execute(context.Background(), session, map[string]interface{}{"days": 400}); res.Status != copilot.StatusError {
		t.Fatalf("result = %+v", res)
	}
}

func TestEmailCampaignMetrics(t *testing.T) {
	d, _, _ := deps()
	tool := toolNamed(t, d, "email_campaign_metrics")
	res := tool.Execute(context.Background(), session, map[string]interface{}{"campaign_id": knownCampaign})
	data, ok := res.Data.(map[string]interface{})
	if res.Status != copilot.StatusOK || !ok || data["name"] != "Black Friday" {
		t.Fatalf("result = %+v", res)
	}
	if res := tool.Execute(context.Background(), session, map[string]interface{}{"campaign_id": "0f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}); res.Status != copilot.StatusError {
		t.Fatalf("unknown campaign = %+v", res)
	}
	if res := tool.Execute(context.Background(), session, map[string]interface{}{"campaign_id": "black-friday"}); res.Status != copilot.StatusError {
		t.Fatalf("invented id = %+v", res)
	}
}
