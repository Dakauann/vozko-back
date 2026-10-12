package copilot

import (
	"errors"
	"strings"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	ActionAsk ActionKind = "ask"

	MaxFormQuestions       = 4
	MaxQuestionRunes       = 280
	MaxQuestionOptionRunes = 80
	MaxQuestionDetailRunes = 120
	MinQuestionOptions     = 2
	MaxQuestionOptions     = 4
)

var ErrInvalidQuestion = errors.New("copilot: the question card cannot be answered with a tap or a short reply")

type QuestionOption struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

type Question struct {
	Text    string           `json:"text"`
	Options []QuestionOption `json:"options"`
}

func NewQuestionCard(questions []Question) (*ActionCard, error) {
	if len(questions) == 0 || len(questions) > MaxFormQuestions {
		return nil, ErrInvalidQuestion
	}
	asked := make(map[string]bool, len(questions))
	cleaned := make([]Question, 0, len(questions))
	for _, question := range questions {
		clean, err := cleanQuestion(question)
		if err != nil || asked[strings.ToLower(clean.Text)] {
			return nil, ErrInvalidQuestion
		}
		asked[strings.ToLower(clean.Text)] = true
		cleaned = append(cleaned, clean)
	}
	return &ActionCard{Kind: ActionAsk, Questions: cleaned}, nil
}

func cleanQuestion(question Question) (Question, error) {
	text, ok := shortText(question.Text, MaxQuestionRunes, false)
	if !ok || len(question.Options) < MinQuestionOptions || len(question.Options) > MaxQuestionOptions {
		return Question{}, ErrInvalidQuestion
	}
	seen := make(map[string]bool, len(question.Options))
	cleaned := make([]QuestionOption, 0, len(question.Options))
	for _, option := range question.Options {
		label, labelOK := shortText(option.Label, MaxQuestionOptionRunes, false)
		detail, detailOK := shortText(option.Detail, MaxQuestionDetailRunes, true)
		if !labelOK || !detailOK || seen[strings.ToLower(label)] {
			return Question{}, ErrInvalidQuestion
		}
		seen[strings.ToLower(label)] = true
		cleaned = append(cleaned, QuestionOption{Label: label, Detail: detail})
	}
	return Question{Text: text, Options: cleaned}, nil
}

func shortText(raw string, maxRunes int, optional bool) (string, bool) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", optional
	}
	return clean, utf8.RuneCountInString(clean) <= maxRunes && !shared.HasLongDash(clean)
}
