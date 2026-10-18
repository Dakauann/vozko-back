package buttonaction

import "testing"

func TestResolvePrefersTheCampaignRouteThenTheTemplateDefault(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	templateRoute := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "vendas"})
	exit := mustAction(t, buttons, 1, KindOptOut, Params{})
	campaignRoute := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "cobranca"})
	routes := CampaignRoutes{CampaignID: "camp-1", Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Routes: []ButtonAction{campaignRoute}}
	settings := settingsWith(templateRoute, exit)

	got := Resolve(tmpl, settings, routes, 0)
	if got.Action == nil || got.Source != SourceCampaign || got.Action.Params.DepartmentID != "cobranca" {
		t.Fatalf("index 0 = %+v", got)
	}
	got = Resolve(tmpl, settings, CampaignRoutes{}, 0)
	if got.Action == nil || got.Source != SourceTemplate || got.Action.Params.DepartmentID != "vendas" {
		t.Fatalf("index 0 without campaign = %+v", got)
	}
	got = Resolve(tmpl, settings, routes, 1)
	if got.Action == nil || got.Action.Kind != KindOptOut || got.Source != SourceTemplate {
		t.Fatalf("index 1 = %+v", got)
	}
}

func TestResolveWithNoActionIsNone(t *testing.T) {
	got := Resolve(promoTemplate(), settingsWith(), CampaignRoutes{}, 0)
	if got.Action != nil || got.Reason != ReasonNoAction {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveOfAButtonThatIsNotAQuickReplyIsNone(t *testing.T) {
	got := Resolve(promoTemplate(), settingsWith(), CampaignRoutes{}, 2)
	if got.Action != nil || got.Reason != ReasonNoMatch {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveNeverLetsACampaignRouteReplaceATemplateExit(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	exit := mustAction(t, buttons, 1, KindOptOut, Params{})
	sneaky := mustAction(t, buttons, 1, KindHandOff, Params{DepartmentID: "vendas"})
	routes := CampaignRoutes{CampaignID: "camp-1", Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Routes: []ButtonAction{sneaky}}

	got := Resolve(tmpl, settingsWith(exit), routes, 1)
	if got.Action == nil || got.Action.Kind != KindOptOut || got.Source != SourceTemplate {
		t.Fatalf("got %+v, want the template exit", got)
	}
}

func TestResolveNeverRunsAnExitKindStoredOnACampaign(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	planted := mustAction(t, buttons, 0, KindOptOut, Params{})
	routes := CampaignRoutes{CampaignID: "camp-1", Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Routes: []ButtonAction{planted}}

	got := Resolve(tmpl, settingsWith(), routes, 0)
	if got.Action != nil {
		t.Fatalf("got %+v, want none", got)
	}
}

func TestResolveIgnoresACampaignRouteOfAnotherTemplate(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	route := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "cobranca"})
	routes := CampaignRoutes{CampaignID: "camp-1", Template: TemplateRef{Name: "outro_modelo", Language: "pt_BR"}, Routes: []ButtonAction{route}}

	got := Resolve(tmpl, settingsWith(), routes, 0)
	if got.Action != nil {
		t.Fatalf("got %+v, want none", got)
	}
}

func TestResolveIgnoresAStaleCampaignRouteAndFallsBackToTheTemplate(t *testing.T) {
	tmpl := templateWith("Ofertas.", "", quickReply("Falar com atendente"))
	templateRoute := mustAction(t, []string{"Falar com atendente"}, 0, KindHandOff, Params{DepartmentID: "vendas"})
	staleRoute := mustAction(t, []string{"Falar com vendas"}, 0, KindHandOff, Params{DepartmentID: "cobranca"})
	routes := CampaignRoutes{CampaignID: "camp-1", Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Routes: []ButtonAction{staleRoute}}

	got := Resolve(tmpl, settingsWith(templateRoute), routes, 0)
	if got.Action == nil || got.Source != SourceTemplate {
		t.Fatalf("got %+v, want the template default", got)
	}
}
