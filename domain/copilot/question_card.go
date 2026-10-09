package copilot

import (
	"errors"
	"strings"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	ActionAsk ActionKind = "ask"

	MaxQuestionRunes       = 280
	MaxQuestionOptionRunes = 80
	MinQuestionOptions     = 2
	MaxQuestionOptions     = 4
)

var ErrInvalidQuestion = errors.New("copilot: the question card is not answerable in one tap")

type Question struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

func NewQuestionCard(text string, options []string) (*ActionCard, error) {
	clean := strings.TrimSpace(text)
	if clean == "" || utf8.RuneCountInString(clean) > MaxQuestionRunes || shared.HasLongDash(clean) {
		return nil, ErrInvalidQuestion
	}
	if len(options) < MinQuestionOptions || len(options) > MaxQuestionOptions {
		return nil, ErrInvalidQuestion
	}
	seen := make(map[string]bool, len(options))
	cleaned := make([]string, 0, len(options))
	for _, option := range options {
		o := strings.TrimSpace(option)
		if o == "" || utf8.RuneCountInString(o) > MaxQuestionOptionRunes || shared.HasLongDash(o) || seen[strings.ToLower(o)] {
			return nil, ErrInvalidQuestion
		}
		seen[strings.ToLower(o)] = true
		cleaned = append(cleaned, o)
	}
	return &ActionCard{Kind: ActionAsk, Question: &Question{Text: clean, Options: cleaned}}, nil
}
