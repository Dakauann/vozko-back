package emailapi_usecase

import (
	"context"

	"vozko/domain/emailapi"
)

func (s *Service) List(ctx context.Context, q emailapi.LogQuery) (emailapi.Page[emailapi.LogEntry], error) {
	normalized, err := q.Normalize()
	if err != nil {
		return emailapi.Page[emailapi.LogEntry]{}, refusal(err)
	}
	rows, err := s.d.Log.List(ctx, normalized)
	if err != nil {
		return emailapi.Page[emailapi.LogEntry]{}, err
	}
	return emailapi.PageOf(rows, normalized.Limit), nil
}

func (s *Service) Detail(ctx context.Context, workspaceID, id string) (emailapi.Detail, error) {
	entry, err := s.d.Log.Get(ctx, workspaceID, id)
	if err != nil {
		return emailapi.Detail{}, refusal(err)
	}
	detail := emailapi.Detail{Entry: *entry}
	if entry.MessageID == "" {
		return detail, nil
	}
	events, err := s.d.Log.Events(ctx, entry.MessageID)
	if err != nil {
		return emailapi.Detail{}, err
	}
	detail.Events = events
	return detail, nil
}
