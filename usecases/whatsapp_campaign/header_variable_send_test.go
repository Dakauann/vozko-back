package whatsapp_campaign_usecase

import (
	"reflect"
	"testing"

	"vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
)

func headerVariableTemplate() *template.Template {
	return &template.Template{
		ID: "tmpl-1", Name: "pedido", Language: "pt_BR", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryMarketing,
		ParameterFormat: template.ParameterFormatNamed,
		Components: []template.TemplateComponent{
			{Type: "HEADER", Format: "TEXT", Text: "Loja {{loja}}"},
			{Type: "BODY", Text: "Olá {{nome}}, seu pedido {{pedido}} saiu"},
		},
	}
}

func TestACampaignSendsTheHeaderVariableInTheHeaderAndTheNamedBodyByName(t *testing.T) {
	rig := newFormConsumerRig("camp-header", headerVariableTemplate(), nil)
	rig.h.entryRepo.entries["entry-1"].Variables = []string{"Prisma", "Ana", "77"}

	rig.deliver(t, "camp-header")

	if len(rig.client.last) != 1 {
		t.Fatalf("sends = %d", len(rig.client.last))
	}
	sent := rig.client.last[0]
	if !reflect.DeepEqual(sent.HeaderTextParams, []string{"Prisma"}) || !reflect.DeepEqual(sent.Parameters, []string{"Ana", "77"}) || !reflect.DeepEqual(sent.ParameterNames, []string{"nome", "pedido"}) {
		t.Fatalf("sent = %+v", sent)
	}
	if status := rig.h.entryRepo.getStatus("entry-1"); status != wce.SendStatusSent {
		t.Fatalf("status = %s", status)
	}
	info, _ := rig.h.entryRepo.entries["entry-1"].Metadata["template_info"].(map[string]interface{})
	if info["body_text"] != "Olá Ana, seu pedido 77 saiu" {
		t.Fatalf("the stored body = %v", info["body_text"])
	}
}

func TestAPositionalHeaderVariableKeepsTheBodyNumbering(t *testing.T) {
	tmpl := headerVariableTemplate()
	tmpl.ParameterFormat = template.ParameterFormatPositional
	tmpl.Components = []template.TemplateComponent{
		{Type: "HEADER", Format: "TEXT", Text: "Loja {{1}}"},
		{Type: "BODY", Text: "Olá {{1}}, seu pedido {{2}} saiu"},
	}
	rig := newFormConsumerRig("camp-positional", tmpl, nil)
	rig.h.entryRepo.entries["entry-1"].Variables = []string{"Prisma", "Ana", "77"}

	rig.deliver(t, "camp-positional")

	sent := rig.client.last[0]
	if !reflect.DeepEqual(sent.HeaderTextParams, []string{"Prisma"}) || !reflect.DeepEqual(sent.Parameters, []string{"Ana", "77"}) {
		t.Fatalf("sent = %+v", sent)
	}
	info, _ := rig.h.entryRepo.entries["entry-1"].Metadata["template_info"].(map[string]interface{})
	if info["body_text"] != "Olá Ana, seu pedido 77 saiu" {
		t.Fatalf("the stored body = %v", info["body_text"])
	}
}
