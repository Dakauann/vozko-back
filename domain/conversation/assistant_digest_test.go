package conversation

import (
	"strings"
	"testing"
	"time"
)

func TestDigestInboxEntryHidesTheNumberAndTrimsThePreview(t *testing.T) {
	at := time.Date(2026, 9, 20, 14, 30, 0, 0, time.UTC)
	entry := InboxEntry{
		EntryID:            "e1",
		EntryType:          "whatsapp",
		CampaignName:       "Black Friday",
		LeadName:           "Maria",
		LeadNumber:         "+5584994409624",
		UnreadCount:        2,
		LastMessagePreview: strings.Repeat("a", DigestPreviewRunes+40),
		LastMessageAt:      at,
		AssignedUsername:   "Ana",
		Stage:              &InboxEntryStage{StageID: "s1", Name: "Proposta"},
		Labels:             []InboxEntryLabel{{LabelID: "l1", Name: "VIP"}},
		ConversationStatus: ConversationStatusOngoing,
	}

	d := DigestInboxEntry(entry)

	if d.Contact != "••••9624" || strings.Contains(d.Customer, "9624") {
		t.Fatalf("contact leaked: %+v", d)
	}
	if got := len([]rune(d.LastMessage)); got != DigestPreviewRunes+1 {
		t.Fatalf("preview runes = %d, want %d", got, DigestPreviewRunes+1)
	}
	if d.Customer != "Maria" || d.Stage != "Proposta" || d.Labels[0] != "VIP" || d.Responsible != "Ana" ||
		d.Status != "ongoing" || d.Campaign != "Black Friday" || d.Unread != 2 || d.LastMessageAt != "2026-09-20T14:30:00Z" {
		t.Fatalf("digest = %+v", d)
	}
}

func TestDigestInboxEntryNamesTheAutomationThatHoldsIt(t *testing.T) {
	d := DigestInboxEntry(InboxEntry{EntryID: "e1", AIHandler: &AIHandler{Kind: "ai", AgentName: "Sofia"}})
	if d.Responsible != "Sofia (IA)" {
		t.Fatalf("responsible = %q", d.Responsible)
	}
}

func TestDigestTranscriptLabelsTurnsAndSkipsNoise(t *testing.T) {
	at := time.Date(2026, 9, 20, 14, 30, 0, 0, time.UTC)
	messages := []*Message{
		{MessageType: MessageTypeUserMessage, Text: "quanto custa?", CreatedAt: at, SenderName: "Maria"},
		{MessageType: MessageTypeToolCall, Text: "{}", CreatedAt: at},
		{MessageType: MessageTypeOperator, Text: "R$ 300", CreatedAt: at.Add(time.Minute), SenderName: "Ana"},
		{MessageType: MessageTypeAIResponse, Text: strings.Repeat("b", DigestMessageRunes+10), CreatedAt: at.Add(2 * time.Minute)},
		nil,
	}

	lines := DigestTranscript(messages)

	if len(lines) != 3 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].From != SpeakerCustomer || lines[0].Name != "" || lines[0].At != "2026-09-20T14:30:00Z" {
		t.Fatalf("customer line = %+v", lines[0])
	}
	if lines[1].From != SpeakerTeam || lines[1].Name != "Ana" || lines[1].Text != "R$ 300" {
		t.Fatalf("team line = %+v", lines[1])
	}
	if got := len([]rune(lines[2].Text)); got != DigestMessageRunes+1 {
		t.Fatalf("long message runes = %d", got)
	}
}
