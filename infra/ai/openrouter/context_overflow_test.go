package openrouter

import (
	"errors"
	"testing"

	"vozko/domain/ai"
)

func TestARequestLongerThanTheWindowIsNamedAsSuch(t *testing.T) {
	for _, message := range []string{
		"This endpoint's maximum context length is 200000 tokens. However, you requested about 230000 tokens",
		"prompt is too long: 210000 tokens > 200000 maximum",
		"input length and max_tokens exceed context limit",
		"context_length_exceeded",
	} {
		if err := classified(errors.New(message)); !errors.Is(err, ai.ErrContextOverflow) || errors.Is(err, ai.ErrProviderTransient) {
			t.Errorf("%q classified as %v, want a context overflow that is not retried as is", message, err)
		}
	}
	if err := classified(errors.New("invalid api key")); errors.Is(err, ai.ErrContextOverflow) {
		t.Fatal("an unrelated failure is not a context overflow")
	}
}
