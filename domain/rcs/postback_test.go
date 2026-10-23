package rcs

import "testing"

func TestPostbackRoundTrip(t *testing.T) {
	for _, key := range []ButtonKey{{Card: -1, Index: 0}, {Card: -1, Index: 10}, {Card: 0, Index: 3}, {Card: 9, Index: 1}} {
		got, ok := ParsePostback(key.Postback())
		if !ok || got != key {
			t.Fatalf("ParsePostback(%q) = %+v, %v", key.Postback(), got, ok)
		}
	}
}

func TestParsePostbackRefusesForeignData(t *testing.T) {
	for _, raw := range []string{"", "hello", "vz:m", "vz:mx", "vz:c1", "vz:c1b", "vz:m-1", "vz:c-1b0", "vz:m3x"} {
		if _, ok := ParsePostback(raw); ok {
			t.Fatalf("ParsePostback(%q) accepted foreign data", raw)
		}
	}
}

func TestButtonAtFindsTheTappedSuggestion(t *testing.T) {
	c := Content{
		Text:        "Oi",
		Suggestions: []Suggestion{reply("Sim"), reply("Não")},
	}
	got, ok := c.ButtonAt(ButtonKey{Card: -1, Index: 1})
	if !ok || got.Label != "Não" {
		t.Fatalf("ButtonAt = %+v, %v", got, ok)
	}
	carousel := Content{Cards: []Card{card("A"), card("B", reply("Comprar"))}}
	got, ok = carousel.ButtonAt(ButtonKey{Card: 1, Index: 0})
	if !ok || got.Label != "Comprar" {
		t.Fatalf("card ButtonAt = %+v, %v", got, ok)
	}
	if _, ok := carousel.ButtonAt(ButtonKey{Card: 5, Index: 0}); ok {
		t.Fatal("a missing card has no button")
	}
}

func TestButtonsListsEveryButtonWithItsKey(t *testing.T) {
	c := Content{Cards: []Card{card("A", reply("X")), card("B", reply("Y"), reply("Z"))}}
	got := c.Buttons()
	if len(got) != 3 || got[2].Key != (ButtonKey{Card: 1, Index: 1}) || got[2].Suggestion.Label != "Z" {
		t.Fatalf("Buttons = %+v", got)
	}
}

func TestWithTrackedPostbacksStampsEveryButton(t *testing.T) {
	c := Content{Text: "Oi", Suggestions: []Suggestion{reply("Sim")}}
	tracked := c.WithTrackedPostbacks()
	if tracked.Suggestions[0].Postback != (ButtonKey{Card: -1, Index: 0}).Postback() {
		t.Fatalf("postback = %q", tracked.Suggestions[0].Postback)
	}
	if c.Suggestions[0].Postback != "pb-Sim" {
		t.Fatal("the template keeps its own postback")
	}
}

func TestOptOutButton(t *testing.T) {
	if !(Suggestion{Kind: SuggestReply, Label: "Sair", Postback: OptOutPostback}).OptsOut() {
		t.Fatal("a reply button carrying the opt-out postback opts out")
	}
	if (Suggestion{Kind: SuggestOpenURL, Label: "Sair", Postback: OptOutPostback}).OptsOut() {
		t.Fatal("only reply buttons opt out")
	}
}
