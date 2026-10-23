package leadsend_usecase

import (
	"reflect"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	"vozko/domain/sheet"
	"vozko/domain/whatsapp/template"
)

func listPathVariables(t *testing.T, csv string, slots int) []string {
	t.Helper()
	list, err := campaign.ReadContactList(sheet.Parse([]byte(csv)), campaign.ContactPhone)
	if err != nil {
		t.Fatal(err)
	}
	mapping := campaign.MapList(list.Layout, campaign.ContactPhone, make([]campaign.BindingSource, slots), nil)
	return mapping.Cells(list.Rows[0])
}

func TestTheLeadsPageSendFillsNamedAndHeaderTemplatesLikeAList(t *testing.T) {
	named := approvedTemplate("Olá {{nome}}, sua compra {{pedido}} saiu")
	named.ID = "tpl-named"
	named.ParameterFormat = template.ParameterFormatNamed
	named.Components = append(named.Components, template.TemplateComponent{Type: "HEADER", Format: "TEXT", Text: "Pedido {{loja}}"})
	cases := []struct {
		name     string
		tmpl     *template.Template
		bindings []campaign.VariableBinding
		csv      string
		header   []string
		body     []string
	}{
		{name: "named with a header", tmpl: named,
			bindings: []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "Prisma"}, {Source: campaign.BindFirstName}, {Source: campaign.BindLiteral, Value: "77"}},
			csv:      "telefone;nome;var1;var2;var3\n5511999900000;Maria;Prisma;Maria;77\n", header: []string{"Prisma"}, body: []string{"Maria", "77"}},
		{name: "numbered header", tmpl: nil,
			bindings: []campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "Prisma"}, {Source: campaign.BindFirstName}},
			csv:      "telefone;nome;var1;var2\n5511999900000;Maria;Prisma;Maria\n", header: []string{"Prisma"}, body: []string{"Maria"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nil)
			templates := h.deps.Templates.(fakeTemplates)
			tmpl := tc.tmpl
			if tmpl == nil {
				tmpl = templates.byID["tpl-header"]
			} else {
				templates.byID[tmpl.ID] = tmpl
			}
			h.seed(1)
			params := templateParams()
			params.TemplateID, params.Bindings = tmpl.ID, tc.bindings
			if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, params); err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			inputs := h.world.parts[h.world.order[0]].official.PhoneInputs
			if len(inputs) != 1 {
				t.Fatalf("inputs = %+v", inputs)
			}
			fromList := listPathVariables(t, tc.csv, tmpl.ParameterCount())
			if !reflect.DeepEqual(inputs[0].Variables, fromList) {
				t.Fatalf("leads page %q, list %q", inputs[0].Variables, fromList)
			}
			body, header := tmpl.SplitParameters(inputs[0].Variables)
			if !reflect.DeepEqual(header, tc.header) || !reflect.DeepEqual(body, tc.body) {
				t.Fatalf("the send reads header %q body %q", header, body)
			}
		})
	}
}
