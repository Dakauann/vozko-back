package conversation

import (
	"encoding/json"
	"testing"
	"time"
)

func TestApplyEditReplacesTheTextAndStampsTheEdit(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		msg       Message
		text      string
		wantText  string
		wantStamp bool
	}{
		{"a new text replaces the old one", Message{Text: "oi"}, " olá ", "olá", true},
		{"an edit without text keeps the old text but is still an edit", Message{Text: "oi"}, "", "oi", true},
		{"a retracted message ignores later edits", Message{Text: "oi", RetractedAt: &at}, "olá", "oi", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.msg
			m.ApplyEdit(c.text, at)
			if m.Text != c.wantText {
				t.Fatalf("text = %q, want %q", m.Text, c.wantText)
			}
			if stamped := m.EditedAt != nil && m.EditedAt.Equal(at); stamped != c.wantStamp {
				t.Fatalf("edited_at = %v, want stamped %v", m.EditedAt, c.wantStamp)
			}
		})
	}
}

func TestRetractKeepsTheFirstRetraction(t *testing.T) {
	first := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	m := &Message{Text: "oi"}
	m.Retract(first)
	m.Retract(first.Add(time.Hour))
	if !m.IsRetracted() || !m.RetractedAt.Equal(first) {
		t.Fatalf("retracted_at = %v, want %v", m.RetractedAt, first)
	}
}

func TestReadableTextIsTheTextElseTheTranscript(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		msg  *Message
		want string
	}{
		{"plain text", &Message{Text: "oi"}, "oi"},
		{"a transcript behind the audio placeholder reads as before", &Message{Text: AudioPlaceholder, Transcript: "quero o plano"}, AudioPlaceholder + " quero o plano"},
		{"a transcript without text reads as audio", &Message{Transcript: " quero o plano "}, AudioPlaceholder + " quero o plano"},
		{"an old row with the transcript in the text is unchanged", &Message{Text: AudioPlaceholder + " quero o plano"}, AudioPlaceholder + " quero o plano"},
		{"a caption keeps the transcript after it", &Message{Text: "segue", Transcript: "quero o plano"}, "segue\n" + AudioPlaceholder + " quero o plano"},
		{"a retracted message reads as nothing", &Message{Text: "oi", Transcript: "x", RetractedAt: &at}, ""},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.msg.ReadableText(); got != c.want {
				t.Fatalf("ReadableText() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTranscribableReadsTheReadableText(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if !(&Message{MessageType: MessageTypeAudio, Transcript: "quero"}).Transcribable() {
		t.Fatal("an audio with a transcript is something the customer said")
	}
	if (&Message{MessageType: MessageTypeUserMessage, Text: "oi", RetractedAt: &at}).Transcribable() {
		t.Fatal("a retracted message must not reach any transcript")
	}
}

func TestPromptContentCarriesTheTranscriptAndSurvivesRichMetadata(t *testing.T) {
	m := &Message{
		Text:       AudioPlaceholder,
		Transcript: "quero o plano",
		Metadata:   json.RawMessage(`{"extracted_text":"nota","reactions":[{"emoji":"👍","direction":"inbound","at":"2026-10-18T12:00:00Z"}]}`),
	}
	want := AudioPlaceholder + " quero o plano\n\nConteúdo extraído:\nnota"
	if got := m.PromptContent(); got != want {
		t.Fatalf("PromptContent() = %q, want %q", got, want)
	}
}
