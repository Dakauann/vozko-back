package container

import (
	"context"
	"time"

	conversation_domain "vozko/domain/conversation"
	cauc "vozko/usecases/audience"
)

type commentEscalationRecipients struct {
	inbox inboxSearcher
}

type inboxSearcher interface {
	SearchInboxEntries(input conversation_domain.SearchInboxInput) ([]conversation_domain.InboxEntry, int64, error)
}

func (a commentEscalationRecipients) ListEscalationRecipients(_ context.Context, in cauc.EscalationRecipientQuery) ([]cauc.EscalationRecipient, error) {
	if a.inbox == nil {
		return nil, nil
	}
	entries, _, err := a.inbox.SearchInboxEntries(conversation_domain.SearchInboxInput{
		UserID:      in.UserID,
		WorkspaceID: in.WorkspaceID,
		Query:       in.Query,
		Page:        1,
		PageSize:    in.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]cauc.EscalationRecipient, 0, len(entries))
	for _, e := range entries {
		if e.Blocked || e.IsGroup {
			continue
		}
		out = append(out, cauc.EscalationRecipient{
			EntryID:       e.EntryID,
			EntryType:     e.EntryType,
			Name:          e.LeadName,
			Number:        e.LeadNumber,
			WindowOpen:    e.WindowOpen,
			LastMessageAt: formatRecipientTime(e.LastMessageAt),
		})
	}
	return out, nil
}

func formatRecipientTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
