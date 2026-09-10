package container

import (
	"context"
	"time"

	conversation_domain "vozko/domain/conversation"
	cauc "vozko/usecases/comment_analysis"
)

// commentEscalationRecipients adapts the CRM's inbox search onto the narrow
// port the comment engine declares for its recipient picker.
//
// Like pipelineStageSeeder and conversationFunnelLister, it lives in the
// composition root because it is the only place allowed to know both sides.
//
// The query is deliberately workspace-wide: an empty CampaignID is the same
// "global" scope the inbox WebSocket uses when no campaign is selected, so
// someone forwarding a hostile comment to their manager does not have to know
// which campaign that person is filed under. The user id still travels, so the
// search returns only conversations that user may already see.
type commentEscalationRecipients struct {
	inbox inboxSearcher
}

// inboxSearcher is the one method of the history provider this needs. Named
// locally rather than taking HistoryProvider whole: that interface carries a
// dozen methods about message history, none of which belong here.
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
		// A blocked contact cannot receive anything, and a group has no single
		// person to forward a warning to.
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
