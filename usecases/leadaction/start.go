package leadaction_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	report_usecase "vozko/usecases/report"
	report_renderers "vozko/usecases/report/renderers"
)

func (s *Service) Start(ctx context.Context, req Request) (Outcome, error) {
	if !leadaction.ValidKey(req.IdempotencyKey) {
		return Outcome{}, leadaction.ErrIdempotencyKeyRequired
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.Params = req.Params.Normalized()
	defs, err := s.authorize(req.Actor, req.Action, req.Params)
	if err != nil {
		return Outcome{}, err
	}
	if err := req.Selection.ValidateConfirmed(); err != nil {
		return Outcome{}, err
	}
	if err := s.checkPhone(req.Actor, req.Action, req.Params); err != nil {
		return Outcome{}, err
	}
	switch {
	case req.Action.Runs():
		return s.startRun(ctx, req, defs)
	case req.Action == leadaction.ActionExport:
		return s.startExport(ctx, req)
	case req.Action == leadaction.ActionCallList:
		return s.startCallList(ctx, req)
	case req.Action.Sends():
		return s.startSend(ctx, req)
	default:
		return s.startAudience(ctx, req)
	}
}

func (s *Service) existingRun(ctx context.Context, req Request, fingerprint string) (*leadaction.Run, error) {
	existing, err := s.deps.Runs.FindByKey(ctx, req.Actor.WorkspaceID, req.IdempotencyKey)
	if errors.Is(err, leadaction.ErrRunNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existing.ActorID != req.Actor.UserID || existing.RequestFingerprint != fingerprint {
		return nil, leadaction.ErrIdempotencyKeyReused
	}
	return existing, nil
}

func (s *Service) confirmed(ctx context.Context, req Request, then func(ctx context.Context, matched int) error) error {
	return s.gated(ctx, func(ctx context.Context) error {
		matched, err := s.deps.Selections.Count(ctx, scopeOf(req.Actor, req.DepartmentID), req.Selection.BeforeExclusions())
		if err != nil {
			return err
		}
		if err := req.Selection.ConfirmCount(matched); err != nil {
			return err
		}
		return then(ctx, matched)
	})
}

func (s *Service) startRun(ctx context.Context, req Request, defs []*customfield.Definition) (Outcome, error) {
	fingerprint := leadaction.RequestFingerprint(req.Action, req.Params, req.Selection)
	existing, err := s.existingRun(ctx, req, fingerprint)
	if err != nil {
		return Outcome{}, err
	}
	if existing != nil {
		run, err := s.redacted(req.Actor, existing)
		return Outcome{Run: run}, err
	}
	edit, err := s.editFor(req.Actor, req.Action, req.Params, defs)
	if err != nil {
		return Outcome{}, err
	}
	runID := derivedID("run", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey, fingerprint)
	var run *leadaction.Run
	err = s.confirmed(ctx, req, func(ctx context.Context, matched int) error {
		frozen, err := s.deps.Selections.Freeze(ctx, scopeOf(req.Actor, req.DepartmentID), req.Selection, runID, edit.PendingFor(req.Selection))
		if err != nil {
			return err
		}
		if frozen.Size == 0 {
			return leadaction.ErrSelectionEmpty
		}
		run, err = leadaction.NewRun(leadaction.RunRequest{
			ID: runID, WorkspaceID: req.Actor.WorkspaceID, ActorID: req.Actor.UserID, IsAdmin: req.Actor.IsAdmin,
			DepartmentID: req.DepartmentID, Action: req.Action, Params: req.Params, IdempotencyKey: req.IdempotencyKey,
			RequestFingerprint: fingerprint, Matched: matched, Selected: frozen.Size,
		}, s.now())
		return err
	})
	if err != nil {
		return Outcome{}, err
	}
	if err := s.deps.Runs.Create(ctx, run); err != nil {
		winner, lookupErr := s.lostRace(ctx, req, fingerprint, err)
		if winner == nil || winner.ID != runID {
			s.dropSnapshot(ctx, req.Actor.WorkspaceID, runID)
		}
		if lookupErr != nil {
			return Outcome{}, lookupErr
		}
		run = winner
	} else {
		runLog(run).Info("lead action: run queued", "actor_id", run.ActorID, "matched", run.Result.Matched, "selected", run.Result.Selected)
	}
	s.launch(run.ID)
	run, err = s.redacted(req.Actor, run)
	return Outcome{Run: run}, err
}

func (s *Service) lostRace(ctx context.Context, req Request, fingerprint string, createErr error) (*leadaction.Run, error) {
	if !errors.Is(createErr, leadaction.ErrRunExists) {
		return nil, createErr
	}
	winner, err := s.existingRun(ctx, req, fingerprint)
	if err != nil {
		return nil, err
	}
	if winner == nil {
		return nil, fmt.Errorf("lead action run of key %s: %w", req.IdempotencyKey, leadaction.ErrRunExists)
	}
	return winner, nil
}

func (s *Service) startExport(ctx context.Context, req Request) (Outcome, error) {
	began := s.now()
	fingerprint := leadaction.RequestFingerprint(req.Action, req.Params, req.Selection)
	snapshotID := derivedID("export", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey, fingerprint)
	scope := scopeOf(req.Actor, req.DepartmentID)
	held, err := s.deps.Selections.SnapshotSize(ctx, req.Actor.WorkspaceID, snapshotID)
	if err != nil {
		return Outcome{}, err
	}
	selected := held
	if held == 0 {
		err = s.confirmed(ctx, req, func(ctx context.Context, matched int) error {
			if req.Selection.Exceeds(matched, leadaction.MaxExportLeads) {
				return fmt.Errorf("%w: at most %d leads per export", leadaction.ErrSelectionTooLarge, leadaction.MaxExportLeads)
			}
			frozen, err := s.deps.Selections.Freeze(ctx, scope, req.Selection, snapshotID, nil)
			selected = frozen.Size
			return err
		})
		if err != nil {
			return Outcome{}, err
		}
	}
	if selected == 0 {
		return Outcome{}, leadaction.ErrSelectionEmpty
	}
	if selected > leadaction.MaxExportLeads {
		s.dropSnapshot(ctx, req.Actor.WorkspaceID, snapshotID)
		return Outcome{}, fmt.Errorf("%w: at most %d leads per export", leadaction.ErrSelectionTooLarge, leadaction.MaxExportLeads)
	}
	params, err := json.Marshal(report_renderers.LeadsParams{
		SnapshotID: snapshotID, Addresses: req.Params.Addresses, Sensitive: req.Params.Sensitive, Selected: selected,
	})
	if err != nil {
		return Outcome{}, err
	}
	job, err := s.deps.Reports.Create(report_usecase.CreateInput{
		WorkspaceID: req.Actor.WorkspaceID, RequestedBy: req.Actor.UserID, RequestedByAdmin: req.Actor.IsAdmin,
		Kind: report.KindLeads, Format: report.FormatCSV, Locale: req.Locale, Params: params,
	})
	if err != nil {
		return Outcome{}, err
	}
	if !job.CreatedAt.Before(began) {
		s.countCreated(req.Action, began)
		actionLog(snapshotID, req.Actor.WorkspaceID, req.Action).Info("lead action: export queued", "actor_id", req.Actor.UserID, "report_id", job.ID, "selected", selected)
	}
	return Outcome{Report: job}, nil
}
