package crmtools

import (
	"context"
	"reflect"
	"testing"

	"vozko/domain/copilot"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type headedTemplates struct{ grantedTemplates }

func (headedTemplates) Get(_ string, id string) (*tmpl.Template, error) {
	return &tmpl.Template{ID: id, Name: "pedido_saiu", Status: tmpl.TemplateStatusApproved, Category: "UTILITY", ParameterFormat: tmpl.ParameterFormatNamed,
		Components: []tmpl.TemplateComponent{{Type: "HEADER", Format: "TEXT", Text: "Loja {{loja}}"}, {Type: "BODY", Text: "Olá {{nome}}, pedido {{pedido}} saiu."}}}, nil
}

func TestSendTemplatePutsTheHeaderValueInTheHeaderNowAndWhenScheduled(t *testing.T) {
	send := &fakePersonTemplates{}
	scheduler := &fakeScheduler{}
	deps := templateSendDeps(send, false)
	deps.Templates, deps.Scheduler = headedTemplates{}, scheduler
	tool := NewSendTemplateTool(deps)
	if res := tool.Execute(context.Background(), tooltest.Member(), templateArgs("Prisma", "Maria", "123")); res.Status != copilot.StatusOK {
		t.Fatalf("send = %+v", res)
	}
	if !reflect.DeepEqual(send.req.Variables, []string{"Maria", "123"}) || !reflect.DeepEqual(send.req.HeaderVariables, []string{"Prisma"}) {
		t.Fatalf("send request = %+v", send.req)
	}
	args := templateArgs("Prisma", "Maria", "123")
	args["scheduled_at"] = "2026-10-10T09:00:00-03:00"
	if res := tool.Execute(context.Background(), tooltest.Member(), args); res.Status != copilot.StatusOK {
		t.Fatalf("schedule = %+v", res)
	}
	content := scheduler.scheduled[0].Template
	if !reflect.DeepEqual(content.BodyParams, []string{"Maria", "123"}) || !reflect.DeepEqual(content.HeaderParams, []string{"Prisma"}) {
		t.Fatalf("scheduled = %+v", content)
	}
}
