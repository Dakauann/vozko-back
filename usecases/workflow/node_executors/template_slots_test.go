package node_executors

import (
	"context"
	"reflect"
	"strings"
	"testing"

	template_domain "vozko/domain/whatsapp/template"
)

type fixedTemplate struct {
	template_domain.Repository
	tmpl *template_domain.Template
}

func (f fixedTemplate) FindByID(string) (*template_domain.Template, error) { return f.tmpl, nil }

func slotTemplate(header, body string, format template_domain.ParameterFormat) *template_domain.Template {
	return &template_domain.Template{
		ID: "t-slots", Name: "aviso", Language: "pt_BR", Status: template_domain.TemplateStatusApproved, WABAId: "waba-1",
		Category: template_domain.TemplateCategoryUtility, ParameterFormat: format,
		Components: []template_domain.TemplateComponent{{Type: "HEADER", Format: "TEXT", Text: header}, {Type: "BODY", Text: body}},
	}
}

func TestAWorkflowTemplateSendsOneValuePerSlotWithTheHeaderInTheHeader(t *testing.T) {
	cases := []struct {
		name   string
		tmpl   *template_domain.Template
		values map[string]string
		body   []string
		header []string
		text   string
	}{
		{name: "numbered, a repeated body variable takes one value", tmpl: slotTemplate("Loja {{1}}", "Oi {{1}}, sim {{1}}, até {{2}}.", template_domain.ParameterFormatPositional),
			values: map[string]string{"header_1": "Prisma", "1": "Ana", "2": "amanhã"}, body: []string{"Ana", "amanhã"}, header: []string{"Prisma"}, text: "Oi Ana, sim Ana, até amanhã."},
		{name: "named", tmpl: slotTemplate("Loja {{loja}}", "Oi {{nome}}, pedido {{pedido}}.", template_domain.ParameterFormatNamed),
			values: map[string]string{"header_loja": "Prisma", "nome": "Ana", "pedido": "77"}, body: []string{"Ana", "77"}, header: []string{"Prisma"}, text: "Oi Ana, pedido 77."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sends, history := &recordingTemplateSends{}, &recordingHistory{}
			sender := templateSender(sends, history)
			sender.deps.TemplateRepo = fixedTemplate{tmpl: tc.tmpl}
			if _, _, err := sender.SendTemplate(context.Background(), waMediaRun(), "t-slots", "", tc.values, nil); err != nil {
				t.Fatal(err)
			}
			if got := sends.sends[0]; !reflect.DeepEqual(got.BodyParams, tc.body) || !reflect.DeepEqual(got.HeaderParams, tc.header) {
				t.Fatalf("billed body %q header %q", got.BodyParams, got.HeaderParams)
			}
			record := history.records[0]
			if record.Text != tc.text || !strings.Contains(string(record.Metadata), "Loja Prisma") {
				t.Fatalf("history = %q %s", record.Text, record.Metadata)
			}
		})
	}
}
