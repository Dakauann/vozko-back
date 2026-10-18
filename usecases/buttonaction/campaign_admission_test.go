package buttonaction_usecase

import (
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

func novemberPromo() *template.Template {
	tmpl := promoTemplate()
	tmpl.ID, tmpl.Name = "tmpl-2", "promo_novembro"
	return tmpl
}

func TestChangingACampaignsTemplateDropsTheRoutesOfTheOldOne(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SaveTemplateActions(ctx, actor(), templateID, 1, optOutAndHandOff()); err != nil {
		t.Fatal(err)
	}
	route := []buttonaction.Input{{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{UserID: "user-ana"}}}
	if _, err := f.svc.SaveCampaignRoutes(ctx, actor(), campaignID, 1, route); err != nil {
		t.Fatal(err)
	}
	f.settings.rows[settingsKey(ws, buttonaction.RefOf(novemberPromo()))] = buttonaction.TemplateSettings{
		WorkspaceID: ws, Template: buttonaction.RefOf(novemberPromo()), Version: 2,
		Actions: []buttonaction.ButtonAction{{Template: buttonaction.RefOf(novemberPromo()), ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut}},
	}

	admission, err := f.svc.AdmitCampaign(ctx, ws, novemberPromo(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecordCampaignAdmission(ctx, ws, campaignID, admission); err != nil {
		t.Fatal(err)
	}

	routes, _ := f.routes.Get(ctx, ws, campaignID)
	if len(routes.Routes) != 0 || routes.Template != buttonaction.RefOf(novemberPromo()) {
		t.Fatalf("routes = %+v", routes)
	}
	resolution := buttonaction.Resolve(novemberPromo(), buttonaction.TemplateSettings{}, routes, 0)
	if resolution.Action != nil {
		t.Fatalf("a route of the old template carried over: %+v", resolution)
	}
}
