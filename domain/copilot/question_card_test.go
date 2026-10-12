package copilot

import (
	"errors"
	"strings"
	"testing"
)

func options(labels ...string) []QuestionOption {
	out := make([]QuestionOption, 0, len(labels))
	for _, label := range labels {
		out = append(out, QuestionOption{Label: label})
	}
	return out
}

func TestQuestionCardOffersShortDistinctOptions(t *testing.T) {
	card, err := NewQuestionCard([]Question{{Text: "  Para onde vai este vídeo?  ", Options: []QuestionOption{{Label: "Reels e Stories", Detail: " Vertical 9:16 "}, {Label: " Feed "}, {Label: "YouTube"}}}})
	if err != nil {
		t.Fatalf("NewQuestionCard() = %v", err)
	}
	if card.Kind != ActionAsk || len(card.Questions) != 1 || card.Questions[0].Text != "Para onde vai este vídeo?" {
		t.Fatalf("card = %+v", card)
	}
	got := card.Questions[0].Options
	if got[0].Label != "Reels e Stories" || got[0].Detail != "Vertical 9:16" || got[1].Label != "Feed" || got[2].Label != "YouTube" {
		t.Fatalf("options = %+v", got)
	}
	if ActionAsk.Valid() {
		t.Fatal("a question is not an offer: offer_action must not show it")
	}
}

func TestQuestionCardCarriesAShortFormOfSeveralQuestions(t *testing.T) {
	form := []Question{
		{Text: "Qual formato?", Options: options("Stories 9:16", "Feed 4:5")},
		{Text: "Quanto tempo?", Options: options("6 s", "15 s", "30 s")},
		{Text: "Qual o convite final?", Options: options("Inscreva-se", "Garanta seu ingresso")},
		{Text: "Tem cores da marca?", Options: options("Usar as da marca", "Pode escolher")},
	}
	card, err := NewQuestionCard(form)
	if err != nil {
		t.Fatalf("NewQuestionCard() = %v", err)
	}
	if len(card.Questions) != MaxFormQuestions || card.Questions[3].Text != "Tem cores da marca?" {
		t.Fatalf("questions = %+v", card.Questions)
	}
}

func TestQuestionCardRefusesWhatTheUserCannotAnswer(t *testing.T) {
	five := make([]Question, 0, MaxFormQuestions+1)
	for i := 0; i <= MaxFormQuestions; i++ {
		five = append(five, Question{Text: strings.Repeat("q", i+1) + "?", Options: options("Sim", "Não")})
	}
	cases := map[string][]Question{
		"no questions":       nil,
		"too many questions": five,
		"repeated question":  {{Text: "Qual duração?", Options: options("15 s", "30 s")}, {Text: " qual duração? ", Options: options("Sim", "Não")}},
		"no question":        {{Text: "", Options: options("Sim", "Não")}},
		"no options":         {{Text: "Qual duração?"}},
		"a single option":    {{Text: "Qual duração?", Options: options("15 s")}},
		"too many options":   {{Text: "Qual duração?", Options: options("1", "2", "3", "4", "5")}},
		"repeated option":    {{Text: "Qual duração?", Options: options("15 s", " 15 S")}},
		"blank option":       {{Text: "Qual duração?", Options: options("15 s", "  ")}},
		"long option":        {{Text: "Qual duração?", Options: options("15 s", strings.Repeat("a", MaxQuestionOptionRunes+1))}},
		"long detail":        {{Text: "Qual duração?", Options: []QuestionOption{{Label: "15 s", Detail: strings.Repeat("a", MaxQuestionDetailRunes+1)}, {Label: "30 s"}}}},
		"dash in a detail":   {{Text: "Qual duração?", Options: []QuestionOption{{Label: "15 s", Detail: "curto — direto"}, {Label: "30 s"}}}},
		"long question":      {{Text: strings.Repeat("a", MaxQuestionRunes+1), Options: options("Sim", "Não")}},
		"dash in an option":  {{Text: "Qual duração?", Options: options("15 — 30 s", "60 s")}},
		"one bad question":   {{Text: "Qual formato?", Options: options("Stories", "Feed")}, {Text: "Quanto tempo?", Options: options("15 s")}},
	}
	for name, questions := range cases {
		if _, err := NewQuestionCard(questions); !errors.Is(err, ErrInvalidQuestion) {
			t.Fatalf("%s: NewQuestionCard() = %v, want ErrInvalidQuestion", name, err)
		}
	}
}
