package buttonaction

import "testing"

func TestMatchButtonByPayload(t *testing.T) {
	tmpl := promoTemplate()
	index, ok := MatchButton(tmpl, "vz1:1", "SAIR")
	if !ok || index != 1 {
		t.Fatalf("got %d, %v; want 1", index, ok)
	}
}

func TestMatchButtonByPayloadWithoutText(t *testing.T) {
	index, ok := MatchButton(promoTemplate(), "vz1:0", "")
	if !ok || index != 0 {
		t.Fatalf("got %d, %v; want 0", index, ok)
	}
}

func TestMatchButtonRefusesAPayloadWhoseTextNoLongerMatches(t *testing.T) {
	if index, ok := MatchButton(promoTemplate(), "vz1:1", "Quero mais"); ok {
		t.Fatalf("got %d, want no match: the tapped label is not the button stored at that index", index)
	}
}

func TestMatchButtonRefusesAPayloadOnANonQuickReply(t *testing.T) {
	for _, payload := range []string{"vz1:2", "vz1:7"} {
		if index, ok := MatchButton(promoTemplate(), payload, "Ver loja"); ok {
			t.Errorf("%s: got %d, want no match", payload, index)
		}
	}
}

func TestMatchButtonByFoldedText(t *testing.T) {
	cases := map[string]struct {
		payload string
		text    string
		want    int
	}{
		"meta default payload": {"SAIR", "SAIR", 1},
		"no payload":           {"", "sair", 1},
		"accents and spaces":   {"", "  falar com atendente ", 0},
	}
	for name, tc := range cases {
		index, ok := MatchButton(promoTemplate(), tc.payload, tc.text)
		if !ok || index != tc.want {
			t.Errorf("%s: got %d, %v; want %d", name, index, ok, tc.want)
		}
	}
}

func TestMatchButtonByTextNeedsExactlyOneButton(t *testing.T) {
	twice := templateWith("Oi", "", quickReply("Sim"), quickReply("SIM"))
	if index, ok := MatchButton(twice, "", "sim"); ok {
		t.Errorf("two buttons with the same folded text: got %d, want no match", index)
	}
	if index, ok := MatchButton(promoTemplate(), "", "parar"); ok {
		t.Errorf("no button: got %d, want no match", index)
	}
	if index, ok := MatchButton(promoTemplate(), "", "Ver loja"); ok {
		t.Errorf("a URL button is not a quick reply: got %d, want no match", index)
	}
	if index, ok := MatchButton(nil, "vz1:0", "SAIR"); ok {
		t.Errorf("no template: got %d, want no match", index)
	}
}
