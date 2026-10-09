package lead_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/lead"
)

var errIncomingMergeIncomplete = errors.New("lead incoming merge: a store and a notifier are required")

type IncomingMerge struct {
	writer recordWriter
}

func NewIncomingMerge(store lead.Store, notifier lead.ChangeNotifier) (*IncomingMerge, error) {
	if store == nil || notifier == nil {
		return nil, errIncomingMergeIncomplete
	}
	return &IncomingMerge{writer: recordWriter{store: store, notifier: notifier}}, nil
}

func (m *IncomingMerge) MergeIncoming(ctx context.Context, workspaceID, leadID string, update lead.LeadUpdate) (*lead.Lead, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if !update.Source.Valid() {
		return nil, lead.ErrLeadSourceInvalid
	}
	return m.writer.retrying(ctx, workspaceID, actor.SystemID, leadID, lead.EventMerged, func(l *lead.Lead) ([]string, error) {
		return l.MergeIncoming(update), nil
	})
}
