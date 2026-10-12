package copilottools

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/shared"
)

const UnknownConversation = "conversa inexistente ou sem acesso"

type conversationTarget struct {
	EntryID   string
	EntryType shared.EntryType
}

func TargetOf(entryID, entryType string) (conversationTarget, error) {
	t := conversationTarget{EntryID: strings.TrimSpace(entryID), EntryType: shared.EntryType(strings.TrimSpace(entryType))}
	if t.EntryID == "" {
		return t, fmt.Errorf("%w: entry_id é obrigatório; copie o de search_conversations", ErrInvalidArgs)
	}
	if !t.EntryType.SupportsConversationView() {
		return t, fmt.Errorf("%w: entry_type desconhecido; copie o de search_conversations", ErrInvalidArgs)
	}
	return t, nil
}

func PersonOf(cc copilot.Context) shared.Person {
	return shared.Person{UserID: cc.UserID, SystemAdmin: cc.SystemAdmin}
}

func ViewerOf(cc copilot.Context) conversation.Viewer {
	return conversation.Viewer{UserID: cc.UserID, WorkspaceID: cc.WorkspaceID, IsAdmin: cc.SystemAdmin}
}

func DescribeConversation(entries conversation.EntryLookup, cc copilot.Context, entryID, entryType string) string {
	target, err := TargetOf(entryID, entryType)
	if err != nil || entries == nil {
		return UnknownConversation
	}
	entry, err := entries.LookupEntry(ViewerOf(cc), target.EntryID, target.EntryType)
	if err != nil || entry == nil {
		return UnknownConversation
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

func KnownID(raw, name, source string) (string, error) {
	id := strings.TrimSpace(raw)
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("%w: %s desconhecido; use o id exato de %s", ErrInvalidArgs, name, source)
	}
	return id, nil
}
