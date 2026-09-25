package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	tmpl "vozko/domain/whatsapp/template"
)

func bodyExamples(t *testing.T, p *copilot.Preview) []string {
	t.Helper()
	if p == nil || p.Kind != copilot.PreviewWhatsAppTemplate {
		t.Fatalf("preview = %+v", p)
	}
	for _, c := range p.Data.(TemplatePreview).Components {
		if c.Type == "BODY" && c.Example != nil && len(c.Example.BodyText) > 0 {
			return c.Example.BodyText[0]
		}
	}
	return nil
}

func TestTemplatePreviewOfFillsTheRealValuesWithoutTouchingTheTemplate(t *testing.T) {
	original := &tmpl.Template{Name: "pedido_saiu", Language: "pt_BR", Category: "UTILITY",
		Components: []tmpl.TemplateComponent{{Type: "BODY", Text: "Olá {{1}}", Example: &tmpl.TemplateExample{BodyText: [][]string{{"Exemplo"}}}}}}
	p := templatePreviewOf(original, []string{"Maria"})
	if p.Components[0].Example.BodyText[0][0] != "Maria" || original.Components[0].Example.BodyText[0][0] != "Exemplo" {
		t.Fatalf("preview %+v original %+v", p.Components[0].Example, original.Components[0].Example)
	}
}

func TestSendTemplatePreviewShowsTheCustomersValues(t *testing.T) {
	tool := NewSendTemplateTool(templateSendDeps(&fakePersonTemplates{}, false)).(copilot.Previewer)
	p := tool.Preview(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "whatsapp", "template_id": knownTemplate, "variables": []interface{}{"Maria", "123"},
	})
	if got := bodyExamples(t, p); len(got) != 2 || got[0] != "Maria" || got[1] != "123" {
		t.Fatalf("examples = %v", got)
	}
	if denied := NewSendTemplateTool(templateSendDeps(&fakePersonTemplates{}, true)).(copilot.Previewer).Preview(context.Background(), member(), map[string]interface{}{"template_id": knownTemplate}); denied != nil {
		t.Fatalf("a template without access was previewed: %+v", denied)
	}
}

func TestCampaignPreviewUsesTheFirstValidRow(t *testing.T) {
	p := NewCreateCampaignTool(newCampaignFixture().deps).(copilot.Previewer).Preview(context.Background(), member(), createCampaignArgsFor(nil))
	if got := bodyExamples(t, p); len(got) != 2 || got[0] != "Maria" || got[1] != "10" {
		t.Fatalf("examples = %v", got)
	}
}

func TestMessagePreviewShowsTheExactText(t *testing.T) {
	tool := NewScheduleMessageTool(ConversationActionDeps{}).(copilot.Previewer)
	p := tool.Preview(context.Background(), member(), map[string]interface{}{
		"entry_id": knownEntry, "entry_type": "instagram", "text": "Oi Maria, tudo certo?", "scheduled_at": "2026-09-26T09:00:00-03:00",
	})
	data, ok := p.Data.(MessagePreview)
	if p.Kind != copilot.PreviewMessage || !ok || data.Text != "Oi Maria, tudo certo?" || data.Channel != "instagram" || data.ScheduledAt == "" {
		t.Fatalf("preview = %+v", p)
	}
}
