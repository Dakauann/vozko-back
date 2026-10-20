package conversation

import (
	"strings"
	"time"
)

const AudioPlaceholder = "[Áudio]"

func (m *Message) ApplyEdit(text string, at time.Time) {
	if m == nil || m.IsRetracted() {
		return
	}
	if edited := strings.TrimSpace(text); edited != "" {
		m.Text = edited
	}
	stamp := at.UTC()
	m.EditedAt = &stamp
}

func (m *Message) Retract(at time.Time) {
	if m == nil || m.IsRetracted() {
		return
	}
	stamp := at.UTC()
	m.RetractedAt = &stamp
}

func (m *Message) IsRetracted() bool {
	return m != nil && m.RetractedAt != nil
}

func (m *Message) SetTranscript(transcript string) {
	if m == nil {
		return
	}
	m.Transcript = strings.TrimSpace(transcript)
}

func SpokenText(transcript string) string {
	return AudioPlaceholder + " " + strings.TrimSpace(transcript)
}

func (m *Message) ReadableText() string {
	if m == nil || m.IsRetracted() {
		return ""
	}
	transcript := strings.TrimSpace(m.Transcript)
	if transcript == "" {
		return m.Text
	}
	spoken := SpokenText(transcript)
	text := strings.TrimSpace(m.Text)
	if text == "" || text == AudioPlaceholder {
		return spoken
	}
	return text + "\n" + spoken
}
