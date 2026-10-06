package copilot

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrChoiceMissing = errors.New("a choice on the approval card was not made")

type ChoiceKind string

const (
	ChoiceImageModel ChoiceKind = "image_model"
	ChoiceMusicModel ChoiceKind = "music_model"
	ChoiceVoiceModel ChoiceKind = "voice_model"
)

type ChoiceField struct {
	Key     string     `json:"key"`
	Kind    ChoiceKind `json:"kind"`
	Default string     `json:"default,omitempty"`
}

type ChoiceAsker interface {
	Choices(ctx context.Context, cc Context, args map[string]interface{}) ([]ChoiceField, error)
}

type Approval struct {
	Secrets map[string]string
	Choices map[string]string
}

func StripChoices(args map[string]interface{}, choices []ChoiceField) map[string]interface{} {
	out := make(map[string]interface{}, len(args))
	for key, value := range args {
		out[key] = value
	}
	for _, choice := range choices {
		delete(out, choice.Key)
	}
	return out
}

func WithChoices(args map[string]interface{}, choices []ChoiceField, provided map[string]string) (map[string]interface{}, error) {
	out := StripChoices(args, choices)
	for _, choice := range choices {
		value := strings.TrimSpace(provided[choice.Key])
		if value == "" {
			return nil, fmt.Errorf("%w: %s", ErrChoiceMissing, choice.Kind)
		}
		out[choice.Key] = value
	}
	return out, nil
}
