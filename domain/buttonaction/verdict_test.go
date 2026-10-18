package buttonaction

import (
	"slices"
	"testing"
)

func mustAction(t *testing.T, tmplButtons []string, index int, kind Kind, params Params) ButtonAction {
	t.Helper()
	action, err := NewButtonAction(templateWith("Oi", "", quickReplies(tmplButtons...)...), Input{ButtonIndex: index, Kind: kind, Params: params})
	if err != nil {
		t.Fatal(err)
	}
	return action
}

func settingsWith(actions ...ButtonAction) TemplateSettings {
	return TemplateSettings{Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Actions: actions, Version: 2}
}

func TestVerdictOkWithAButtonThatHasAnExitAction(t *testing.T) {
	buttons := []string{"Falar com atendente", "Não quero receber"}
	tmpl := templateWith("Ofertas da semana.", "", quickReplies(buttons...)...)
	cases := map[Kind]Params{KindOptOut: {}, KindConsentGrant: {Purpose: "ofertas"}}
	for kind, params := range cases {
		verdict := ExitVerdict(tmpl, settingsWith(mustAction(t, buttons, 1, kind, params)), CampaignRoutes{})
		if verdict.State != VerdictOK {
			t.Fatalf("%s: state = %s, want ok", kind, verdict.State)
		}
		if len(verdict.Buttons) != 2 || verdict.Buttons[1].Action == nil || verdict.Buttons[1].Action.Kind != kind || verdict.Buttons[1].Source != SourceTemplate {
			t.Fatalf("%s: buttons = %+v", kind, verdict.Buttons)
		}
		if verdict.Buttons[0].Action != nil || verdict.Buttons[0].Source != SourceNone {
			t.Fatalf("%s: a button with no action shows none: %+v", kind, verdict.Buttons[0])
		}
	}
}

func TestVerdictOkWithAnExitCommandTheLeadSees(t *testing.T) {
	cases := map[string]struct {
		body    string
		footer  string
		buttons []string
		want    []string
	}{
		"body instruction":      {"Para cancelar, digite /sair.", "", []string{"Quero saber mais"}, []string{"sair"}},
		"footer instruction":    {"Ofertas.", "Responda /parar para não receber", nil, []string{"parar"}},
		"quick reply command":   {"Ofertas.", "", []string{"Quero saber mais", "/Sair"}, []string{"Sair"}},
		"body and footer words": {"Digite /sair", "Ou /parar", nil, []string{"sair", "parar"}},
	}
	for name, tc := range cases {
		verdict := ExitVerdict(templateWith(tc.body, tc.footer, quickReplies(tc.buttons...)...), settingsWith(), CampaignRoutes{})
		if verdict.State != VerdictOK || !slices.Equal(verdict.ExitWords, tc.want) {
			t.Errorf("%s: verdict = %+v, want ok with %q", name, verdict, tc.want)
		}
	}
}

func TestVerdictNoneWhenTheLeadSeesNoExit(t *testing.T) {
	cases := map[string]struct {
		body    string
		footer  string
		buttons []string
	}{
		"exit looking button without action": {"Ofertas.", "", []string{"SAIR", "Não quero receber"}},
		"footer naming a word without slash": {"Ofertas.", "Responda SAIR para não receber", nil},
		"a link is not a command":            {"Veja https://loja.com/sair", "", []string{"Quero saber mais"}},
	}
	for name, tc := range cases {
		verdict := ExitVerdict(templateWith(tc.body, tc.footer, quickReplies(tc.buttons...)...), settingsWith(), CampaignRoutes{})
		if verdict.State != VerdictNone {
			t.Errorf("%s: state = %s, want none", name, verdict.State)
		}
	}
}

func TestVerdictNoneWhenTheExitActionWentStale(t *testing.T) {
	stale := mustAction(t, []string{"Não quero"}, 0, KindOptOut, Params{})
	tmpl := templateWith("Ofertas.", "", quickReply("Não quero receber"))
	verdict := ExitVerdict(tmpl, settingsWith(stale), CampaignRoutes{})
	if verdict.State != VerdictNone || !verdict.Buttons[0].Stale || verdict.Buttons[0].Action != nil {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestVerdictListsEveryButtonWithItsSourceAndNonNilLists(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	templateRoute := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "vendas"})
	campaignRoute := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "cobranca"})
	routes := CampaignRoutes{CampaignID: "camp-1", Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Routes: []ButtonAction{campaignRoute}}

	verdict := ExitVerdict(tmpl, settingsWith(templateRoute), routes)
	first := verdict.Buttons[0]
	if first.Source != SourceCampaign || first.Action == nil || first.Action.Params.DepartmentID != "cobranca" {
		t.Fatalf("first = %+v", first)
	}
	if verdict.Buttons[1].Source != SourceNone || verdict.Buttons[1].Action != nil {
		t.Fatalf("second = %+v", verdict.Buttons[1])
	}
	if verdict.ExitWords == nil || verdict.State != VerdictNone {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestOnlyAnExitActionLeadsOut(t *testing.T) {
	optOut, handOff := ButtonAction{Kind: KindOptOut}, ButtonAction{Kind: KindHandOff}
	cases := map[string]struct {
		route ButtonRoute
		want  bool
	}{
		"exit action":         {ButtonRoute{Text: "Não quero", Action: &optOut}, true},
		"route action":        {ButtonRoute{Text: "Falar", Action: &handOff}, false},
		"no action":           {ButtonRoute{Text: "SAIR"}, false},
		"command without act": {ButtonRoute{Text: "/sair"}, false},
	}
	for name, tc := range cases {
		if got := tc.route.LeadsOut(); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
