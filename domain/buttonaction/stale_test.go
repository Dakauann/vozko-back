package buttonaction

import (
	"testing"

	"vozko/domain/whatsapp/template"
)

func quickReplies(texts ...string) []template.TemplateButton {
	out := make([]template.TemplateButton, 0, len(texts))
	for _, text := range texts {
		out = append(out, quickReply(text))
	}
	return out
}

func TestAnActionIsStaleWhenTheButtonTextChanged(t *testing.T) {
	action, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 1, Kind: KindOptOut})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		buttons []string
		stale   bool
	}{
		"same text":           {[]string{"Falar com atendente", "SAIR"}, false},
		"case and spaces":     {[]string{"Falar com atendente", " sair "}, false},
		"renamed":             {[]string{"Falar com atendente", "Quero mais"}, true},
		"moved to index zero": {[]string{"SAIR"}, true},
	}
	for name, tc := range cases {
		tmpl := templateWith("Oi", "", quickReplies(tc.buttons...)...)
		if got := action.StaleAgainst(tmpl); got != tc.stale {
			t.Errorf("%s: stale = %v, want %v", name, got, tc.stale)
		}
	}
}

func TestAStaleActionResolvesToNone(t *testing.T) {
	action, err := NewButtonAction(promoTemplate(), Input{ButtonIndex: 0, Kind: KindHandOff, Params: Params{DepartmentID: "dep-1"}})
	if err != nil {
		t.Fatal(err)
	}
	settings := TemplateSettings{Template: TemplateRef{Name: "promo_outubro", Language: "pt_BR"}, Actions: []ButtonAction{action}}

	renamed := templateWith("Ofertas", "", quickReply("Ver catálogo"), quickReply("SAIR"))
	got := Resolve(renamed, settings, CampaignRoutes{}, 0)
	if got.Action != nil || got.Reason != ReasonStale {
		t.Fatalf("resolution = %+v, want none because stale", got)
	}
}
