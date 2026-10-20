package brand

import (
	"errors"
	"testing"
)

func TestParseGender_AcceptsOnlyTheTwoConfiguredValues(t *testing.T) {
	cases := []struct {
		raw  string
		want Gender
		ok   bool
	}{
		{"feminine", Feminine, true},
		{"masculine", Masculine, true},
		{"  Feminine ", Feminine, true},
		{"", "", false},
		{"neutral", "", false},
		{"f", "", false},
		{"a", "", false},
	}
	for _, c := range cases {
		got, err := ParseGender(c.raw)
		if c.ok && (err != nil || got != c.want) {
			t.Fatalf("ParseGender(%q) = %q, %v; want %q", c.raw, got, err, c.want)
		}
		if !c.ok && !errors.Is(err, ErrUnknownGender) {
			t.Fatalf("ParseGender(%q) must refuse, got %q, %v", c.raw, got, err)
		}
	}
}

func TestPortuguesePhrases_ContractTheArticleWithTheBrandGender(t *testing.T) {
	cases := []struct {
		gender Gender
		want   Phrases
	}{
		{Feminine, Phrases{TheBrand: "a Acme", OfBrand: "da Acme", InBrand: "na Acme", ToBrand: "à Acme", ByBrand: "pela Acme", ABrand: "uma Acme"}},
		{Masculine, Phrases{TheBrand: "o Acme", OfBrand: "do Acme", InBrand: "no Acme", ToBrand: "ao Acme", ByBrand: "pelo Acme", ABrand: "um Acme"}},
	}
	for _, c := range cases {
		if got := PortuguesePhrases("Acme", c.gender); got != c.want {
			t.Fatalf("PortuguesePhrases(%s) = %+v, want %+v", c.gender, got, c.want)
		}
	}
}

func TestSpanishPhrases_ContractTheArticleWithTheBrandGender(t *testing.T) {
	cases := []struct {
		gender Gender
		want   Phrases
	}{
		{Feminine, Phrases{TheBrand: "la Acme", OfBrand: "de la Acme", InBrand: "en la Acme", ToBrand: "a la Acme", ByBrand: "por la Acme", ABrand: "una Acme"}},
		{Masculine, Phrases{TheBrand: "el Acme", OfBrand: "del Acme", InBrand: "en el Acme", ToBrand: "al Acme", ByBrand: "por el Acme", ABrand: "un Acme"}},
	}
	for _, c := range cases {
		if got := SpanishPhrases("Acme", c.gender); got != c.want {
			t.Fatalf("SpanishPhrases(%s) = %+v, want %+v", c.gender, got, c.want)
		}
	}
}

func TestPhrases_WithoutAKnownGenderNeverGuessAnArticle(t *testing.T) {
	bare := Phrases{TheBrand: "Acme", OfBrand: "de Acme", InBrand: "em Acme", ToBrand: "a Acme", ByBrand: "por Acme", ABrand: "Acme"}
	if got := PortuguesePhrases("Acme", ""); got != bare {
		t.Fatalf("an unknown gender must not pick o or a, got %+v", got)
	}
	bareSpanish := Phrases{TheBrand: "Acme", OfBrand: "de Acme", InBrand: "en Acme", ToBrand: "a Acme", ByBrand: "por Acme", ABrand: "Acme"}
	if got := SpanishPhrases("Acme", "neutral"); got != bareSpanish {
		t.Fatalf("an unknown gender must not pick el or la, got %+v", got)
	}
}

func TestPhrasesCapitalized_StartsEachPhraseForTheBeginningOfASentence(t *testing.T) {
	got := PortuguesePhrases("Acme", Feminine).Capitalized()
	want := Phrases{TheBrand: "A Acme", OfBrand: "Da Acme", InBrand: "Na Acme", ToBrand: "À Acme", ByBrand: "Pela Acme", ABrand: "Uma Acme"}
	if got != want {
		t.Fatalf("Capitalized() = %+v, want %+v", got, want)
	}
}

func TestBrandPhrases_UseTheConfiguredNameAndGender(t *testing.T) {
	b := Brand{Name: "Acme", Gender: Feminine}
	if got := b.Portuguese().OfBrand; got != "da Acme" {
		t.Fatalf("Portuguese().OfBrand = %q", got)
	}
	if got := b.Spanish().ToBrand; got != "a la Acme" {
		t.Fatalf("Spanish().ToBrand = %q", got)
	}
}
