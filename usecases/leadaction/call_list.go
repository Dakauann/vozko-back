package leadaction_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/calls/calllist"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
)

func callListDraft(id string, p *leadaction.CallListParams) calllist.Draft {
	if p == nil {
		return calllist.Draft{ID: id}
	}
	return calllist.Draft{
		ID: id, Name: p.Name, AssigneeIDs: p.AssigneeIDs,
		Phone: calllist.PhoneChoice{Source: calllist.PhoneSource(p.PhoneSource), Label: lead.PhoneLabel(p.PhoneLabel)},
	}
}

func callListKey(workspaceID, reservationID string) string {
	return "leadaction:call_list:" + workspaceID + ":" + reservationID
}

func (s *Service) startCallList(ctx context.Context, req Request) (Outcome, error) {
	began := s.now()
	fingerprint := leadaction.RequestFingerprint(req.Action, req.Params, req.Selection)
	key := callListKey(req.Actor.WorkspaceID, derivedID("call-list-key", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey))
	if err := s.reserveFingerprint(key, fingerprint); err != nil {
		return Outcome{}, err
	}
	listID := derivedID("call_list", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey, fingerprint)
	existing, err := s.deps.CallLists.View(ctx, req.Actor, listID)
	if err == nil {
		return Outcome{CallList: &existing}, nil
	}
	if !errors.Is(err, calllist.ErrListNotFound) {
		return Outcome{}, err
	}
	prepared, err := s.deps.CallLists.Prepare(ctx, req.Actor, callListDraft(listID, req.Params.CallList))
	if err != nil {
		return Outcome{}, err
	}
	selected := 0
	err = s.confirmed(ctx, req, func(ctx context.Context, matched int) error {
		if req.Selection.Exceeds(matched, calllist.MaxItems) {
			return fmt.Errorf("%w: at most %d leads per call list", calllist.ErrSelectionTooLarge, calllist.MaxItems)
		}
		frozen, err := s.deps.Selections.Freeze(ctx, scopeOf(req.Actor, req.DepartmentID), req.Selection, listID, nil)
		selected = frozen.Size
		return err
	})
	if err != nil {
		return Outcome{}, err
	}
	if selected == 0 {
		return Outcome{}, leadaction.ErrSelectionEmpty
	}
	l, err := s.deps.CallLists.CreateFromSnapshot(ctx, req.Actor, prepared, selected)
	if err != nil {
		s.dropSnapshot(ctx, req.Actor.WorkspaceID, listID)
		return Outcome{}, err
	}
	s.countCreated(req.Action, began)
	actionLog(listID, req.Actor.WorkspaceID, req.Action).Info("lead action: call list created", "actor_id", req.Actor.UserID, "selected", selected)
	return Outcome{CallList: &l}, nil
}

func (s *Service) checkCallList(ctx context.Context, req Request) error {
	if req.Action != leadaction.ActionCallList {
		return nil
	}
	_, err := s.deps.CallLists.Prepare(ctx, req.Actor, callListDraft("", req.Params.CallList))
	return err
}
