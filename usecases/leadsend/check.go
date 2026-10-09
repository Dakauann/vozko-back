package leadsend_usecase

import (
	"context"
	"fmt"

	"vozko/domain/leadaction"
)

func (s *Service) Check(ctx context.Context, a Actor, action leadaction.Action, p leadaction.SendParams) error {
	plan, err := s.plan(ctx, a, action, p)
	if err != nil {
		return err
	}
	_, err = s.creation(ctx, a, plan.params)
	return err
}

func (s *Service) HasParts(ctx context.Context, a Actor, action leadaction.Action, base string) (bool, error) {
	if !action.Sends() {
		return false, fmt.Errorf("%w: %q", leadaction.ErrUnknownAction, action)
	}
	found, err := s.deps.Store.KeyedParts(ctx, action.Channel(), a.WorkspaceID, base)
	if err != nil {
		return false, err
	}
	return len(found) > 0, nil
}
