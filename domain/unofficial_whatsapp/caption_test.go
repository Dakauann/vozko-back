package unofficial_whatsapp

import (
	"encoding/json"
	"testing"
)

// A photo with something written under it.
//
// The words live in `content.caption`, never in `text` — contentField has always
// decoded them, but the only fallback that read it was the interactive-reply
// one, gated on an option id a captioned photo does not have. So the caption was
// parsed, held in memory, and dropped: the operator saw the image and nothing
// the customer had written about it.

// normalizeOne is the whole decode path for a single provider message.
func normalizeOne(t *testing.T, msg map[string]any) *Event {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1", "data": msg,
	})
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	env, err := DecodeEnvelope(body)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	events := NormalizeEnvelope("inst-1", env)
	if len(events) != 1 {
		t.Fatalf("normalized %d events, want 1", len(events))
	}
	return events[0]
}

func imageWithCaption(caption string) map[string]any {
	return map[string]any{
		"messageid":   "msg-1",
		"chatid":      "5511999999999@s.whatsapp.net",
		"sender":      "5511999999999@s.whatsapp.net",
		"messageType": "image",
		"mimetype":    "image/jpeg",
		// Empty, which is exactly what the provider sends for a captioned image.
		"text": "",
		"content": map[string]any{
			"caption": caption,
		},
		"messageTimestamp": int64(1767225600000),
	}
}

// The bug.
func TestImageCaptionIsRead(t *testing.T) {
	const caption = "Plataforma de liderança já está atualizada com os disparos ✅✅"

	ev := normalizeOne(t, imageWithCaption(caption))

	if ev.Text != caption {
		t.Fatalf("Text = %q, want the caption — the customer's words were dropped", ev.Text)
	}
	if ev.Media != MediaImage {
		t.Errorf("Media = %v, want image; the caption must not cost the attachment", ev.Media)
	}
}

// Every media kind that can carry one. A video and a document take captions too,
// and a fix that only covered photos would leave the same hole one type over.
func TestCaptionIsReadForEveryMediaKind(t *testing.T) {
	kinds := map[string]MediaKind{
		"image":    MediaImage,
		"video":    MediaVideo,
		"document": MediaDocument,
	}
	for providerType, want := range kinds {
		t.Run(providerType, func(t *testing.T) {
			msg := imageWithCaption("olha isso")
			msg["messageType"] = providerType

			ev := normalizeOne(t, msg)
			if ev.Text != "olha isso" {
				t.Errorf("Text = %q, want the caption", ev.Text)
			}
			if ev.Media != want {
				t.Errorf("Media = %v, want %v", ev.Media, want)
			}
		})
	}
}

// `text` still wins when the provider populates it. The caption is a FALLBACK:
// preferring content over text would change what a plain message records, and
// this channel has no replay endpoint to fix that with.
func TestExplicitTextOutranksTheContentField(t *testing.T) {
	msg := imageWithCaption("a legenda")
	msg["text"] = "o texto"

	if ev := normalizeOne(t, msg); ev.Text != "o texto" {
		t.Errorf("Text = %q, want the explicit text", ev.Text)
	}
}

// content as a bare STRING is the plain-text shape, and it must still resolve —
// contentField accepts both and this is the half that is not an object.
func TestStringContentIsRead(t *testing.T) {
	msg := imageWithCaption("")
	msg["content"] = "bom dia"

	if ev := normalizeOne(t, msg); ev.Text != "bom dia" {
		t.Errorf("Text = %q, want the string content", ev.Text)
	}
}

// A photo sent with NO caption stays empty here rather than inventing words.
// The usecase layer supplies a placeholder naming what arrived; a normalizer
// that guessed would make that impossible to distinguish from a real message.
func TestUncaptionedMediaStaysEmpty(t *testing.T) {
	msg := imageWithCaption("")
	delete(msg, "content")

	if ev := normalizeOne(t, msg); ev.Text != "" {
		t.Errorf("Text = %q, want empty", ev.Text)
	}
}

// The interactive path keeps its own precedence: a tapped button's VISIBLE label
// outranks the raw content, because "Sim, quero agendar" is what the customer
// chose and the content field may hold the machine id. Pinned because the
// caption fallback runs after it and must not reorder it.
func TestButtonLabelStillOutranksContent(t *testing.T) {
	ev := normalizeOne(t, map[string]any{
		"messageid":        "msg-2",
		"chatid":           "5511999999999@s.whatsapp.net",
		"sender":           "5511999999999@s.whatsapp.net",
		"messageType":      "text",
		"text":             "",
		"buttonOrListid":   "sim_agendar",
		"buttonOrListText": "Sim, quero agendar",
		"content":          map[string]any{"text": "sim_agendar"},
		"messageTimestamp": int64(1767225600000),
	})

	if ev.Text != "Sim, quero agendar" {
		t.Errorf("Text = %q, want the button's visible label", ev.Text)
	}
	if ev.OptionID != "sim_agendar" {
		t.Errorf("OptionID = %q", ev.OptionID)
	}
}
