package unofficial_whatsapp

import (
	"strings"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

// A message must never be persisted with nothing in it.
//
// Persistence rejects an empty body and the consumer treats the rejection as
// retryable, so an event that can never gain content is retried to exhaustion
// and permanently lost — this provider has no replay endpoint. Two paths reach
// that state: an attachment whose download failed (storeAttachment degrades on
// purpose) and any message type the normalizer has no text for yet.
func TestPlaceholderNamesWhatArrived(t *testing.T) {
	cases := []struct {
		name  string
		event uw.Event
		want  string
	}{
		{"image", uw.Event{Media: uw.MediaImage}, "[imagem]"},
		{"video", uw.Event{Media: uw.MediaVideo}, "[vídeo]"},
		{"audio", uw.Event{Media: uw.MediaAudio}, "[áudio]"},
		{"voice note is audio to a reader", uw.Event{Media: uw.MediaVoice}, "[áudio]"},
		{"document", uw.Event{Media: uw.MediaDocument}, "[documento]"},
		{"sticker", uw.Event{Media: uw.MediaSticker}, "[figurinha]"},
		{"a named file with no recognised kind", uw.Event{FileName: "contrato.pdf"}, "[contrato.pdf]"},
		{"nothing identifiable at all", uw.Event{}, "[mensagem sem conteúdo]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := placeholderForEmptyMessage(&tc.event)
			if got != tc.want {
				t.Errorf("placeholder = %q, want %q", got, tc.want)
			}
			// The point of the placeholder is that it is never empty; an empty
			// one would fail persistence exactly like the case it replaces.
			if strings.TrimSpace(got) == "" {
				t.Error("placeholder is blank, which is the bug it exists to prevent")
			}
		})
	}
}

// Every media kind the domain defines must produce a placeholder, so a kind
// added later cannot silently fall through to the generic text.
func TestEveryMediaKindHasItsOwnPlaceholder(t *testing.T) {
	generic := placeholderForEmptyMessage(&uw.Event{})
	kinds := []uw.MediaKind{
		uw.MediaImage, uw.MediaVideo, uw.MediaAudio,
		uw.MediaVoice, uw.MediaDocument, uw.MediaSticker,
	}
	for _, kind := range kinds {
		if got := placeholderForEmptyMessage(&uw.Event{Media: kind}); got == generic {
			t.Errorf("media kind %q falls through to the generic placeholder %q", kind, generic)
		}
	}
}
