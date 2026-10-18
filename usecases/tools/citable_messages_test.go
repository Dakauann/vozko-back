package tools_usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type recentMessagesStub struct {
	messages []*conversation.Message
	err      error
	asked    []conversation.ListMessagesInput
}

func (s *recentMessagesStub) ListByEntryPaginated(in conversation.ListMessagesInput) ([]*conversation.Message, error) {
	s.asked = append(s.asked, in)
	return s.messages, s.err
}

func citableConversation() *recentMessagesStub {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	return &recentMessagesStub{messages: []*conversation.Message{
		{ID: "m-4", Text: "pode parar\ncom isso", SentBy: conversation.SentByContact("5511"), CreatedAt: at.Add(4 * time.Minute)},
		{ID: "m-3", Text: "chamada", MessageType: conversation.MessageTypeToolCall, SentBy: conversation.SentByAI("agent-1"), CreatedAt: at.Add(3 * time.Minute)},
		{ID: "m-2", Text: "Quer receber novidades?", SentBy: conversation.SentByAI("agent-1"), CreatedAt: at.Add(2 * time.Minute)},
		{ID: "m-1", Text: "Bom dia, aqui é a Ana", SentBy: conversation.SentByPerson("user-1"), CreatedAt: at.Add(time.Minute)},
		{ID: "m-0", Text: "Oferta da semana", SentBy: conversation.SentByAI("agent-9"), CreatedAt: at},
	}}
}

func TestOnlyTheConsentSkillsCiteMessages(t *testing.T) {
	if !CitesMessages([]string{"manage_lead_memory", RecordOptOutToolName}) || !CitesMessages([]string{RecordConsentToolName}) {
		t.Fatal("the consent skills cite messages by id")
	}
	if CitesMessages([]string{"manage_lead_memory", "update_lead_profile"}) || CitesMessages(nil) {
		t.Fatal("agents without the consent skills get no message ids")
	}
}

func TestCitableMessagesListTheRecentConversationOldestFirst(t *testing.T) {
	reader := citableConversation()
	note := CitableMessagesNote(reader, CitableConversation{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, AgentID: "agent-1"})

	if len(reader.asked) != 1 || reader.asked[0].EntryID != "entry-1" || reader.asked[0].EntryType != shared.EntryTypeWhatsApp || reader.asked[0].Limit != CitableMessageWindow {
		t.Fatalf("asked %+v", reader.asked)
	}
	lines := []string{
		"[m-0] equipe: Oferta da semana",
		"[m-1] equipe: Bom dia, aqui é a Ana",
		"[m-2] você: Quer receber novidades?",
		"[m-4] lead: pode parar com isso",
	}
	last := -1
	for _, line := range lines {
		at := strings.Index(note, line)
		if at < 0 || at < last {
			t.Fatalf("note must list %q in order:\n%s", line, note)
		}
		last = at
	}
	if strings.Contains(note, "m-3") {
		t.Fatalf("tool steps are not messages anyone can cite:\n%s", note)
	}
}

func TestCitableMessagesShortenLongTexts(t *testing.T) {
	reader := &recentMessagesStub{messages: []*conversation.Message{
		{ID: "m-1", Text: strings.Repeat("palavra ", 100), SentBy: conversation.SentByContact("5511"), CreatedAt: time.Now()},
	}}
	note := CitableMessagesNote(reader, CitableConversation{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp, AgentID: "agent-1"})
	for _, line := range strings.Split(note, "\n") {
		if strings.HasPrefix(line, "[m-1]") && len([]rune(line)) > citableTextRunes+len("[m-1] lead: ")+1 {
			t.Fatalf("line too long (%d runes): %s", len([]rune(line)), line)
		}
	}
}

func TestCitableMessagesSayWhenTheyCannotBeListed(t *testing.T) {
	failing := &recentMessagesStub{err: errors.New("database down")}
	for name, note := range map[string]string{
		"a read failure":    CitableMessagesNote(failing, CitableConversation{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}),
		"no conversation":   CitableMessagesNote(citableConversation(), CitableConversation{}),
		"no message reader": CitableMessagesNote(nil, CitableConversation{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}),
	} {
		if !strings.Contains(note, citableUnavailable) || strings.Contains(note, "[m-") {
			t.Errorf("%s: note = %q", name, note)
		}
	}
}
