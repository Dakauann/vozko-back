package conversation_usecase

import (
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type EntryBuilder interface {
	BuildInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error)
}

type entryLookup struct {
	authorizer conversation.ConversationAuthorizer
	builder    EntryBuilder
}

func NewEntryLookup(authorizer conversation.ConversationAuthorizer, builder EntryBuilder) conversation.EntryLookup {
	return &entryLookup{authorizer: authorizer, builder: builder}
}

func (l *entryLookup) LookupEntry(viewer conversation.Viewer, entryID string, entryType shared.EntryType) (*conversation.InboxEntry, error) {
	if l.authorizer == nil || !l.authorizer.CanAccessEntry(viewer.UserID, viewer.WorkspaceID, entryID, string(entryType), viewer.IsAdmin) {
		return nil, conversation.ErrUnauthorized
	}
	entry, err := l.builder.BuildInboxEntry(entryID, string(entryType))
	if err != nil {
		return nil, fmt.Errorf("inbox entry %s: %w", entryID, err)
	}
	if entry == nil {
		return nil, conversation.ErrConversationNotFound
	}
	return entry, nil
}
