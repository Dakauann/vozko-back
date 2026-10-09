package leadaction_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	"vozko/domain/selection"
	wd "vozko/domain/workspace/workspace_department"
	leadsend_usecase "vozko/usecases/leadsend"
)

const (
	SendKeyRetention = 24 * time.Hour
	SendPrepareLease = 15 * time.Minute
)

type Sends interface {
	Check(ctx context.Context, a Actor, action leadaction.Action, p leadaction.SendParams) error
	Quote(ctx context.Context, a Actor, action leadaction.Action, p leadaction.SendParams, selected int) (*campaign.SendQuote, error)
	Existing(ctx context.Context, a Actor, departments *wd.DepartmentFilter, action leadaction.Action, base string) (*campaign.SendReview, error)
	Prepare(ctx context.Context, req leadsend_usecase.PrepareRequest) (*campaign.SendReview, error)
	HasParts(ctx context.Context, a Actor, action leadaction.Action, base string) (bool, error)
}

func sendKey(workspaceID, reservationID string) string {
	return "leadaction:send:" + workspaceID + ":" + reservationID
}

func sendLeaseKey(workspaceID, base string) string {
	return "leadaction:send:prepare:" + workspaceID + ":" + base
}

func (s *Service) sendReservation(req Request) string {
	return sendKey(req.Actor.WorkspaceID, derivedID("send-key", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey))
}

func (s *Service) sends() (Sends, error) {
	if s.deps.Sends == nil {
		return nil, leadaction.ErrUnavailable
	}
	return s.deps.Sends, nil
}

func (s *Service) previewSend(ctx context.Context, req Request) (*leadaction.Preview, error) {
	sends, err := s.sends()
	if err != nil {
		return nil, err
	}
	if err := sends.Check(ctx, req.Actor, req.Action, *req.Params.Send); err != nil {
		return nil, err
	}
	p := &leadaction.Preview{
		ID: s.deps.NewID(), WorkspaceID: req.Actor.WorkspaceID, ActorID: req.Actor.UserID, Action: req.Action,
		Status: leadaction.PreviewDone, UpdatedAt: s.now(),
		Result: leadaction.PreviewResult{Fingerprint: selection.Fingerprint(req.Selection.EffectiveFilter()), Skipped: map[leadaction.SkipReason]int{}},
	}
	scope := scopeOf(req.Actor, req.DepartmentID)
	err = s.gated(ctx, func(ctx context.Context) error {
		matched, err := s.deps.Selections.Count(ctx, scope, req.Selection.BeforeExclusions())
		if err != nil {
			return err
		}
		p.Result.Matched, p.Result.ExpectedCount = matched, req.Selection.ConfirmableCount(matched)
		selected, err := s.deps.Selections.Selected(ctx, scope, req.Selection)
		p.Result.Selected, p.Result.Eligible = selected, selected
		return err
	})
	if err != nil {
		return nil, err
	}
	p.Send, err = sends.Quote(ctx, req.Actor, req.Action, *req.Params.Send, p.Result.Selected)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) reserveFingerprint(key, fingerprint string) error {
	reserved, err := s.deps.State.SetNX(key, fingerprint, SendKeyRetention)
	if err != nil {
		return err
	}
	if reserved {
		return nil
	}
	stored, err := s.deps.State.GetString(key)
	if err != nil {
		return err
	}
	if stored != fingerprint {
		return leadaction.ErrIdempotencyKeyReused
	}
	return nil
}

func (s *Service) startSend(ctx context.Context, req Request) (Outcome, error) {
	sends, err := s.sends()
	if err != nil {
		return Outcome{}, err
	}
	if err := sends.Check(ctx, req.Actor, req.Action, *req.Params.Send); err != nil {
		return Outcome{}, err
	}
	fingerprint := leadaction.RequestFingerprint(req.Action, req.Params, req.Selection)
	reservation := s.sendReservation(req)
	if err := s.reserveFingerprint(reservation, fingerprint); err != nil {
		return Outcome{}, err
	}
	base := derivedID("send", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey, fingerprint)
	outcome, err := s.leasedSend(ctx, sends, req, base)
	if err != nil && !errors.Is(err, campaign.ErrSendPreparing) {
		s.releaseUnusedKey(ctx, sends, req, reservation, fingerprint, base)
	}
	return outcome, err
}

func (s *Service) releaseUnusedKey(ctx context.Context, sends Sends, req Request, reservation, fingerprint, base string) {
	ctx = context.WithoutCancel(ctx)
	holds, err := sends.HasParts(ctx, req.Actor, req.Action, base)
	if err != nil || holds {
		return
	}
	stored, err := s.deps.State.GetString(reservation)
	if err != nil || stored != fingerprint {
		return
	}
	if err := s.deps.State.Del(reservation); err != nil {
		actionLog(base, req.Actor.WorkspaceID, req.Action).Warn("lead action: the send key stays reserved until it expires", "error", err)
	}
}

func (s *Service) leasedSend(ctx context.Context, sends Sends, req Request, base string) (Outcome, error) {
	lease := sendLeaseKey(req.Actor.WorkspaceID, base)
	leased, err := s.deps.State.SetNX(lease, "1", SendPrepareLease)
	if err != nil {
		return Outcome{}, err
	}
	if !leased {
		return Outcome{}, campaign.ErrSendPreparing
	}
	defer func() {
		if err := s.deps.State.Del(lease); err != nil {
			actionLog(base, req.Actor.WorkspaceID, req.Action).Warn("lead action: the preparation lease of the send stays until it expires", "error", err)
		}
	}()
	return s.prepareSend(ctx, sends, req, base)
}

func (s *Service) sizeFits(ctx context.Context, req Request, matched int) error {
	if campaign.PartsNeeded(matched) <= 1 {
		return nil
	}
	selected, err := s.deps.Selections.Selected(ctx, scopeOf(req.Actor, req.DepartmentID), req.Selection)
	if err != nil {
		return err
	}
	_, err = campaign.CheckSelectionSize(selected, req.Params.Send.Split)
	return err
}

func (s *Service) prepareSend(ctx context.Context, sends Sends, req Request, base string) (Outcome, error) {
	existing, err := sends.Existing(ctx, req.Actor, req.DepartmentFilter, req.Action, base)
	if err != nil {
		return Outcome{}, err
	}
	if existing != nil {
		return Outcome{Send: existing}, nil
	}
	err = s.confirmed(ctx, req, func(ctx context.Context, matched int) error {
		if err := s.sizeFits(ctx, req, matched); err != nil {
			return err
		}
		frozen, err := s.deps.Selections.Freeze(ctx, scopeOf(req.Actor, req.DepartmentID), req.Selection, base, nil)
		if err != nil {
			return err
		}
		if frozen.Size == 0 {
			return leadaction.ErrSelectionEmpty
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	review, err := sends.Prepare(ctx, leadsend_usecase.PrepareRequest{
		Actor: req.Actor, Departments: req.DepartmentFilter, Action: req.Action, Params: *req.Params.Send, SnapshotID: base, Base: base,
	})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Send: review}, nil
}
