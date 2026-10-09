package mediagen

import "testing"

func TestSpokenAsWrittenIgnoresOnlyCaseAndPunctuation(t *testing.T) {
	script := "A vozko é uma empresa que veio para mudar o mundo dos crms, e plataformas de obtenção e manutenção de leads e bases"
	cases := map[string]bool{
		"A vozko é uma empresa que veio para mudar o mundo dos CRMs, e plataformas de obtenção e manutenção de leads e bases.":       true,
		"Claro! A vozko é uma empresa que veio para mudar o mundo dos crms, e plataformas de obtenção e manutenção de leads e bases": false,
		"A vozko é uma empresa que veio para mudar o mundo dos crms e plataformas de obtencao e manutencao de leads e bases":         false,
		"A vozko é uma empresa que veio para mudar o mundo dos crms":                                                                 false,
		"": false,
	}
	for transcript, want := range cases {
		if got := SpokenAsWritten(script, transcript); got != want {
			t.Errorf("%q: got %v, want %v", transcript, got, want)
		}
	}
	if SpokenAsWritten("", "") {
		t.Fatal("an empty script is never read as written")
	}
}
