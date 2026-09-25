package copilottools

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/shared"
)

const unknownConversation = "conversa inexistente ou sem acesso"

type conversationTarget struct {
	EntryID   string
	EntryType shared.EntryType
}

func targetOf(entryID, entryType string) (conversationTarget, error) {
	t := conversationTarget{EntryID: strings.TrimSpace(entryID), EntryType: shared.EntryType(strings.TrimSpace(entryType))}
	if t.EntryID == "" {
		return t, fmt.Errorf("%w: entry_id é obrigatório; copie o de search_conversations", errInvalidArgs)
	}
	if !t.EntryType.SupportsConversationView() {
		return t, fmt.Errorf("%w: entry_type desconhecido; copie o de search_conversations", errInvalidArgs)
	}
	return t, nil
}

func personOf(cc copilot.Context) shared.Person {
	return shared.Person{UserID: cc.UserID, SystemAdmin: cc.SystemAdmin}
}

func viewerOf(cc copilot.Context) conversation.Viewer {
	return conversation.Viewer{UserID: cc.UserID, WorkspaceID: cc.WorkspaceID, IsAdmin: cc.SystemAdmin}
}

func describeConversation(entries conversation.EntryLookup, cc copilot.Context, entryID, entryType string) string {
	target, err := targetOf(entryID, entryType)
	if err != nil || entries == nil {
		return unknownConversation
	}
	entry, err := entries.LookupEntry(viewerOf(cc), target.EntryID, target.EntryType)
	if err != nil || entry == nil {
		return unknownConversation
	}
	name := strings.TrimSpace(entry.LeadName)
	contact := shared.MaskContact(entry.LeadNumber)
	switch {
	case name != "" && contact != "":
		return name + " (" + contact + ")"
	case name != "":
		return name
	case contact != "":
		return contact
	}
	return "conversa sem nome"
}

func knownID(raw, name, source string) (string, error) {
	id := strings.TrimSpace(raw)
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("%w: %s desconhecido; use o id exato de %s", errInvalidArgs, name, source)
	}
	return id, nil
}
