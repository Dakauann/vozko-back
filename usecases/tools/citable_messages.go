package tools_usecase

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

const (
	CitableMessageWindow = 15
	citableTextRunes     = 200
	citableHeading       = `Mensagens citáveis (para record_opt_out e record_consent, use o id entre colchetes; "você" são as suas mensagens):`
	citableUnavailable   = "Mensagens citáveis indisponíveis nesta resposta: não há ids para citar."
)

type RecentMessages interface {
	ListByEntryPaginated(in conversation.ListMessagesInput) ([]*conversation.Message, error)
}

type CitableConversation struct {
	EntryID   string
	EntryType shared.EntryType
	AgentID   string
}

func CitesMessages(toolNames []string) bool {
	return slices.ContainsFunc(toolNames, func(name string) bool {
		name = strings.ToLower(strings.TrimSpace(name))
		return name == RecordOptOutToolName || name == RecordConsentToolName
	})
}

func CitableMessagesNote(reader RecentMessages, at CitableConversation) string {
	if reader == nil || strings.TrimSpace(at.EntryID) == "" || at.EntryType == "" {
		return citableUnavailable
	}
	recent, err := reader.ListByEntryPaginated(conversation.ListMessagesInput{EntryID: at.EntryID, EntryType: at.EntryType, Limit: CitableMessageWindow})
	if err != nil {
		log.Printf("[consent-tools] listing citable messages of %s %s failed: %v", at.EntryType, at.EntryID, err)
		return citableUnavailable
	}
	slices.SortStableFunc(recent, func(a, b *conversation.Message) int { return a.CreatedAt.Compare(b.CreatedAt) })
	lines := []string{citableHeading}
	for _, m := range recent {
		if m == nil || !m.Transcribable() {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", m.ID, citableAuthor(m, at.AgentID), citableText(m.Text)))
	}
	return strings.Join(lines, "\n")
}

func citableAuthor(m *conversation.Message, agentID string) string {
	switch {
	case m.FromCustomer():
		return "lead"
	case m.SentBy.Kind() == conversation.SenderAI && agentID != "" && m.SentBy.Participant() == agentID:
		return "você"
	default:
		return "equipe"
	}
}

func citableText(text string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > citableTextRunes {
		return string(runes[:citableTextRunes]) + "…"
	}
	return string(runes)
}
