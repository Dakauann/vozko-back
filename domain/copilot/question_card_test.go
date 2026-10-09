package copilot

import (
	"errors"
	"strings"
	"testing"
)

func TestQuestionCardOffersShortDistinctOptions(t *testing.T) {
	card, err := NewQuestionCard("  Para onde vai este vídeo?  ", []string{"Reels e Stories", " Feed ", "YouTube"})
	if err != nil {
		t.Fatalf("NewQuestionCard() = %v", err)
	}
	if card.Kind != ActionAsk || card.Question == nil || card.Question.Text != "Para onde vai este vídeo?" {
		t.Fatalf("card = %+v", card)
	}
	if got := strings.Join(card.Question.Options, "|"); got != "Reels e Stories|Feed|YouTube" {
		t.Fatalf("options = %q", got)
	}
	if ActionAsk.Valid() {
		t.Fatal("a question is not an offer: offer_action must not show it")
	}
}

func TestQuestionCardRefusesWhatTheUserCannotAnswerInOneTap(t *testing.T) {
	cases := map[string]struct {
		text    string
		options []string
	}{
		"no question":       {"", []string{"Sim", "Não"}},
		"no options":        {"Qual duração?", nil},
		"a single option":   {"Qual duração?", []string{"15 s"}},
		"too many options":  {"Qual duração?", []string{"1", "2", "3", "4", "5"}},
		"repeated option":   {"Qual duração?", []string{"15 s", " 15 s"}},
		"blank option":      {"Qual duração?", []string{"15 s", "  "}},
		"long option":       {"Qual duração?", []string{"15 s", strings.Repeat("a", MaxQuestionOptionRunes+1)}},
		"long question":     {strings.Repeat("a", MaxQuestionRunes+1), []string{"Sim", "Não"}},
		"dash in an option": {"Qual duração?", []string{"15 — 30 s"}},
	}
	for name, tc := range cases {
		if _, err := NewQuestionCard(tc.text, tc.options); !errors.Is(err, ErrInvalidQuestion) {
			t.Fatalf("%s: NewQuestionCard() = %v, want ErrInvalidQuestion", name, err)
		}
	}
}
