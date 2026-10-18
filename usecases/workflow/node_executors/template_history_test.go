package node_executors

import (
	"context"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
	"vozko/domain/leadexit"
	template_domain "vozko/domain/whatsapp/template"
)

type promoTemplates struct {
	template_domain.Repository
}

func promoTemplate() *template_domain.Template {
	return &template_domain.Template{
		ID: "t-promo", Name: "promo_outubro", Language: "pt_BR", Status: template_domain.TemplateStatusApproved, WABAId: "waba-1",
		Category: template_domain.TemplateCategoryMarketing,
		Components: []template_domain.TemplateComponent{
			{Type: "BODY", Text: "Oi {{1}}, ofertas da semana."},
			{Type: "BUTTONS", Buttons: []template_domain.TemplateButton{{Type: "QUICK_REPLY", Text: "Quero"}, {Type: "QUICK_REPLY", Text: "SAIR"}}},
		},
	}
}

func (promoTemplates) FindByID(string) (*template_domain.Template, error) {
	return promoTemplate(), nil
}

func TestAWorkflowSentTemplateIsStoredWithItsTemplateSoATapOnItResolves(t *testing.T) {
	history := &recordingHistory{}
	sender := templateSender(&recordingTemplateSends{}, history)
	sender.deps.TemplateRepo = promoTemplates{}

	if _, _, err := sender.SendTemplate(context.Background(), waMediaRun(), "t-promo", "", map[string]string{"1": "Ana"}, nil); err != nil {
		t.Fatal(err)
	}

	if len(history.records) != 1 {
		t.Fatalf("records = %+v", history.records)
	}
	record := history.records[0]
	if record.MessageType != conversation.MessageTypeTemplate || record.Text != "Oi Ana, ofertas da semana." {
		t.Fatalf("record = %+v", record)
	}
	wamid := record.MessageID
	stored := &conversation.Message{MessageType: record.MessageType, Metadata: record.Metadata, To: record.To, WhatsAppMessageID: &wamid}
	sent, ok := leadexit.TemplateOfMessage(stored)
	if !ok || sent.Template != buttonaction.NewTemplateRef("promo_outubro", "pt_BR") || sent.MessageID != "wamid.t" || sent.RecipientNumber != "5511999999999" {
		t.Fatalf("the stored message does not name its template: %+v, %v", sent, ok)
	}
	if index, matched := buttonaction.MatchButton(promoTemplate(), template_domain.QuickReplyPayload(1), "SAIR"); !matched || index != 1 {
		t.Fatalf("the tap on SAIR = %d, %v", index, matched)
	}
}
