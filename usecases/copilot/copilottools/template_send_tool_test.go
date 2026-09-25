package copilottools

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	tmpl "vozko/domain/whatsapp/template"
)

const knownTemplate = "2b3c4d5e-6f70-4812-9a3b-4c5d6e7f8091"

type fakePersonTemplates struct {
	by    shared.Person
	req   conversation.TemplateSendRequest
	calls int
	err   error
}

func (f *fakePersonTemplates) Execute(by shared.Person, in conversation.TemplateSendRequest) (string, error) {
	f.calls++
	f.by, f.req = by, in
	return "msg-1", f.err
}

type grantedTemplates struct{ denied bool }

func (g grantedTemplates) List(string, tmpl.ListInput) (*shared.PaginatedResult[*tmpl.Template], error) {
	return nil, nil
}

func (g grantedTemplates) Get(_ string, id string) (*tmpl.Template, error) {
	if g.denied {
		return nil, tmpl.ErrTemplateAccessDenied
	}
	return &tmpl.Template{ID: id, Name: "pedido_saiu", Status: tmpl.TemplateStatusApproved, Category: "UTILITY",
		Components: []tmpl.TemplateComponent{{Type: "BODY", Text: "Olá {{1}}, pedido {{2}} saiu."}}}, nil
}

type fakeCosts struct{}

func (fakeCosts) GetTemplateCostMicros(string, string) (int64, error) { return 8500, nil }

func templateSendDeps(send *fakePersonTemplates, denied bool) TemplateSendDeps {
	return TemplateSendDeps{Send: send, Templates: grantedTemplates{denied: denied}, Costs: fakeCosts{}, Entries: &fakeEntries{}}
}

func templateArgs(variables ...interface{}) map[string]interface{} {
	return map[string]interface{}{"entry_id": knownEntry, "entry_type": "whatsapp", "template_id": knownTemplate, "variables": variables}
}

func TestSendTemplateNeedsApprovalAndTheReopenPermission(t *testing.T) {
	if m := NewSendTemplateTool(templateSendDeps(&fakePersonTemplates{}, false)).Meta(); !m.Mutating || m.Resource != "conversations" || m.Action != "reopen" {
		t.Fatalf("meta = %+v", m)
	}
}

func TestSendTemplateShowsTheCostBeforeApproval(t *testing.T) {
	fields := NewSendTemplateTool(templateSendDeps(&fakePersonTemplates{}, false)).(copilot.Describer).Describe(context.Background(), member(), templateArgs("Maria", "123"))
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["template"] != "pedido_saiu" || got["cost"] != "US$ 0.0085" || got["conversation"] != "Maria (••••9624)" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestSendTemplateChecksTheVariablesBeforeSending(t *testing.T) {
	send := &fakePersonTemplates{}
	res := NewSendTemplateTool(templateSendDeps(send, false)).Execute(context.Background(), member(), templateArgs("Maria"))
	if res.Status != copilot.StatusError || send.calls != 0 {
		t.Fatalf("status %s calls %d", res.Status, send.calls)
	}
}

func TestSendTemplateSendsAsTheUser(t *testing.T) {
	send := &fakePersonTemplates{}
	res := NewSendTemplateTool(templateSendDeps(send, false)).Execute(context.Background(), member(), templateArgs("Maria", "123"))
	if res.Status != copilot.StatusOK || send.by.UserID != "u-1" || send.req.TemplateID != knownTemplate || len(send.req.Variables) != 2 {
		t.Fatalf("status %s: %s by %+v req %+v", res.Status, res.Message, send.by, send.req)
	}
}

func TestSendTemplateRefusesATemplateTheWorkspaceCannotUse(t *testing.T) {
	send := &fakePersonTemplates{}
	res := NewSendTemplateTool(templateSendDeps(send, true)).Execute(context.Background(), member(), templateArgs("Maria", "123"))
	if res.Status != copilot.StatusDenied || send.calls != 0 {
		t.Fatalf("status %s calls %d", res.Status, send.calls)
	}
}

func TestSendTemplateExplainsRefusals(t *testing.T) {
	for _, err := range []error{conversation.ErrTemplateNotGranted, tmpl.ErrTemplatePhoneMismatch, conversation.ErrUnauthorized} {
		res := NewSendTemplateTool(templateSendDeps(&fakePersonTemplates{err: err}, false)).Execute(context.Background(), member(), templateArgs("Maria", "123"))
		if res.Status == copilot.StatusOK || res.Message == "falha ao enviar o modelo" {
			t.Fatalf("%v: %+v", err, res)
		}
	}
}

func (g grantedTemplates) Create(string, string, tmpl.CreateTemplateInput) (*tmpl.CreateTemplateOutput, error) {
	return nil, nil
}
