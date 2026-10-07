package copilottools

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	tmpl "vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
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
	for _, err := range []error{wo.ErrTemplateForbidden, tmpl.ErrTemplatePhoneMismatch, conversation.ErrUnauthorized} {
		res := NewSendTemplateTool(templateSendDeps(&fakePersonTemplates{err: err}, false)).Execute(context.Background(), member(), templateArgs("Maria", "123"))
		if res.Status == copilot.StatusOK || res.Message == "falha ao enviar o modelo" {
			t.Fatalf("%v: %+v", err, res)
		}
	}
}

func (g grantedTemplates) Create(string, string, tmpl.CreateTemplateInput) (*tmpl.CreateTemplateOutput, error) {
	return nil, nil
}

func TestSendTemplateSchedulesAsTheUser(t *testing.T) {
	send := &fakePersonTemplates{}
	scheduler := &fakeScheduler{}
	deps := templateSendDeps(send, false)
	deps.Scheduler = scheduler
	args := templateArgs("Maria", "123")
	args["scheduled_at"] = "2026-10-10T09:00:00-03:00"
	tool := NewSendTemplateTool(deps)
	res := tool.Execute(context.Background(), member(), args)
	if res.Status != copilot.StatusOK || send.calls != 0 || len(scheduler.scheduled) != 1 {
		t.Fatalf("result %+v, sends %d, schedules %+v", res, send.calls, scheduler.scheduled)
	}
	in := scheduler.scheduled[0]
	if scheduler.by.UserID != "u-1" || in.WorkspaceID != member().WorkspaceID || in.Template.ID != knownTemplate || len(in.Template.BodyParams) != 2 || in.Text != "" {
		t.Fatalf("by %+v input %+v", scheduler.by, in)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), member(), args)
	for _, field := range fields {
		if field.Key == "scheduledAt" && field.Value == args["scheduled_at"] {
			return
		}
	}
	t.Fatal("approval omits scheduled time")
}

func TestScheduledTemplateNeverFallsBackToImmediateSend(t *testing.T) {
	for _, at := range []string{"invalid", "2026-10-10T09:00:00", " ", "2026-10-10T09:00:00-03:00"} {
		send := &fakePersonTemplates{}
		args := templateArgs("Maria", "123")
		args["scheduled_at"] = at
		res := NewSendTemplateTool(templateSendDeps(send, false)).Execute(context.Background(), member(), args)
		if res.Status != copilot.StatusError || send.calls != 0 {
			t.Fatalf("time %q result %+v sends %d", at, res, send.calls)
		}
	}
}

func TestScheduledTemplateRefusesPermissionFailureWithoutSending(t *testing.T) {
	send := &fakePersonTemplates{}
	deps := templateSendDeps(send, false)
	deps.Scheduler = &fakeScheduler{err: sm.ErrTemplatePermission}
	args := templateArgs("Maria", "123")
	args["scheduled_at"] = "2026-10-10T09:00:00-03:00"
	res := NewSendTemplateTool(deps).Execute(context.Background(), member(), args)
	if res.Status != copilot.StatusDenied || send.calls != 0 {
		t.Fatalf("result %+v sends %d", res, send.calls)
	}
}
