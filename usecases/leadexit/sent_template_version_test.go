package leadexit_usecase

import (
	"context"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

func TestATapOnAnOlderTemplateOfTheCampaignResolvesByTheSentNameAndLanguage(t *testing.T) {
	resolver := sentTemplatesFixture(t, fakeMessages{byWamid: map[string]*conversation.Message{
		"wamid.campaign": storedTemplate("entry-1", "wamid.campaign", conversation.SentByCampaign("camp-1")),
	}})
	resolver.deps.Templates = fakeTemplateNames{"tmpl-1": {ID: "tmpl-1", Name: "promo_novembro", Language: "pt_BR"}}

	sent, err := resolver.SentTemplate(context.Background(), "wamid.campaign")

	if err != nil || sent == nil {
		t.Fatalf("got %+v, %v", sent, err)
	}
	if sent.TemplateID != "" || sent.Template != promoRef || sent.CampaignID != "camp-1" {
		t.Fatalf("the tap must resolve against the template that was sent: %+v", sent)
	}
}

func TestATapOnTheCampaignsCurrentTemplateKeepsItsID(t *testing.T) {
	resolver := sentTemplatesFixture(t, fakeMessages{byWamid: map[string]*conversation.Message{
		"wamid.campaign": storedTemplate("entry-1", "wamid.campaign", conversation.SentByCampaign("camp-1")),
	}})
	resolver.deps.Templates = fakeTemplateNames{"tmpl-1": {ID: "tmpl-1", Name: "Promo_Outubro", Language: "pt_BR"}}

	sent, err := resolver.SentTemplate(context.Background(), "wamid.campaign")

	if err != nil || sent == nil || sent.TemplateID != "tmpl-1" || sent.Template != buttonaction.RefOf(&template.Template{Name: "promo_outubro", Language: "pt_BR"}) {
		t.Fatalf("got %+v, %v", sent, err)
	}
}
