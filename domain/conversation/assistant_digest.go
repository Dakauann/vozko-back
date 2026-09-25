package conversation

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	DigestPreviewRunes = 160
	DigestMessageRunes = 500
)

type Speaker string

const (
	SpeakerCustomer Speaker = "customer"
	SpeakerTeam     Speaker = "team"
)

type EntryDigest struct {
	EntryID       string   `json:"entry_id"`
	EntryType     string   `json:"entry_type"`
	Customer      string   `json:"customer"`
	Contact       string   `json:"contact,omitempty"`
	Status        string   `json:"status,omitempty"`
	Stage         string   `json:"stage,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	Responsible   string   `json:"responsible,omitempty"`
	Campaign      string   `json:"campaign,omitempty"`
	Unread        int64    `json:"unread"`
	LastMessageAt string   `json:"last_message_at,omitempty"`
	LastMessage   string   `json:"last_message,omitempty"`
}

type TranscriptLine struct {
	At   string  `json:"at"`
	From Speaker `json:"from"`
	Name string  `json:"name,omitempty"`
	Text string  `json:"text"`
}

func DigestInboxEntry(e InboxEntry) EntryDigest {
	d := EntryDigest{
		EntryID:       e.EntryID,
		EntryType:     e.EntryType,
		Customer:      e.LeadName,
		Contact:       shared.MaskContact(e.LeadNumber),
		Status:        string(e.ConversationStatus),
		Responsible:   responsibleName(e),
		Campaign:      e.CampaignName,
		Unread:        e.UnreadCount,
		LastMessageAt: digestTime(e.LastMessageAt),
		LastMessage:   clip(e.LastMessagePreview, DigestPreviewRunes),
	}
	if e.Stage != nil {
		d.Stage = e.Stage.Name
	}
	for _, label := range e.Labels {
		d.Labels = append(d.Labels, label.Name)
	}
	return d
}

func DigestTranscript(messages []*Message) []TranscriptLine {
	lines := make([]TranscriptLine, 0, len(messages))
	for _, m := range messages {
		if !m.Transcribable() {
			continue
		}
		line := TranscriptLine{At: digestTime(m.CreatedAt), From: SpeakerCustomer, Text: clip(m.Text, DigestMessageRunes)}
		if !m.FromCustomer() {
			line.From = SpeakerTeam
			line.Name = m.SenderName
		}
		lines = append(lines, line)
	}
	return lines
}

func responsibleName(e InboxEntry) string {
	if e.AIHandler != nil {
		name := e.AIHandler.AgentName
		if name == "" {
			name = e.AIHandler.WorkflowName
		}
		if name != "" {
			return name + " (IA)"
		}
	}
	return e.AssignedUsername
}

func digestTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func clip(s string, max int) string {
	text, cut := shared.TruncateRunes(strings.TrimSpace(s), max)
	if cut {
		return text + "…"
	}
	return text
}
