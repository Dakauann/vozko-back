package conversation_usecase

import (
	"errors"
	"fmt"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type HistorySource interface {
	GetHistory(entryID string, entryType shared.EntryType, limit int) ([]*conversation.Message, bool, int64, error)
	GetHistoryBefore(entryID string, entryType shared.EntryType, before time.Time, limit int) ([]*conversation.Message, bool, error)
}

var errHistoryUnavailable = errors.New("conversation: history source not configured")

type historyReader struct {
	authorizer conversation.ConversationAuthorizer
	history    HistorySource
}

func NewHistoryReader(authorizer conversation.ConversationAuthorizer, history HistorySource) conversation.HistoryReader {
	return &historyReader{authorizer: authorizer, history: history}
}

func (r *historyReader) ReadHistory(q conversation.HistoryQuery) (conversation.HistoryPage, error) {
	if q.EntryID == "" {
		return conversation.HistoryPage{}, conversation.ErrEntryIDRequired
	}
	if !q.EntryType.SupportsConversationView() {
		return conversation.HistoryPage{}, conversation.ErrEntryTypeInvalid
	}
	if r.authorizer == nil || !r.authorizer.CanAccessEntry(q.Viewer.UserID, q.Viewer.WorkspaceID, q.EntryID, string(q.EntryType), q.Viewer.IsAdmin) {
		return conversation.HistoryPage{}, conversation.ErrUnauthorized
	}
	if r.history == nil {
		return conversation.HistoryPage{}, errHistoryUnavailable
	}
	limit := conversation.HistoryPageSize(q.Limit)
	if q.Before != nil {
		messages, hasMore, err := r.history.GetHistoryBefore(q.EntryID, q.EntryType, *q.Before, limit)
		if err != nil {
			return conversation.HistoryPage{}, fmt.Errorf("history of %s before %s: %w", q.EntryID, q.Before.Format(time.RFC3339), err)
		}
		return conversation.HistoryPage{Messages: messages, HasMore: hasMore}, nil
	}
	messages, hasMore, total, err := r.history.GetHistory(q.EntryID, q.EntryType, limit)
	if err != nil {
		return conversation.HistoryPage{}, fmt.Errorf("history of %s: %w", q.EntryID, err)
	}
	return conversation.HistoryPage{Messages: messages, HasMore: hasMore, Total: total}, nil
}
