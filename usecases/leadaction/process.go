package leadaction_usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
)

const (
	metaSaveEvery     = 25
	metaInlineWait    = 5 * time.Second
	sweepLimit        = 20
	snapshotSweepPage = 5000
	snapshotSweeps    = 20
)

var errMetaDeferred = errors.New("lead action: the Meta block waits for the rate limiter")

func (s *Service) launch(id string) {
	s.deps.Background(func() {
		_ = s.Process(context.Background(), id)
	})
}

func (s *Service) Process(ctx context.Context, id string) error {
	token := s.deps.NewID()
	run, err := s.deps.Runs.Claim(ctx, id, token, s.now())
	if errors.Is(err, leadaction.ErrClaimLost) || errors.Is(err, leadaction.ErrRunNotFound) {
		return nil
	}
	if err != nil {
		slog.Warn("lead action: the run could not be claimed", "run_id", id, "error", err)
		return err
	}
	err = s.execute(ctx, run, token)
	switch {
	case err == nil, errors.Is(err, errMetaDeferred), errors.Is(err, leadaction.ErrClaimLost):
		return nil
	case permanent(err):
		runLog(run).Warn("lead action: the run failed", "attempt", run.Attempts, "error", err)
		s.finish(ctx, run, token, failureOf(err))
		return err
	default:
		runLog(run).Warn("lead action: the run stopped and goes back to the queue", "attempt", run.Attempts, "error", err)
		run.Release(s.now())
		if saveErr := s.deps.Runs.Save(context.WithoutCancel(ctx), run, token); saveErr != nil {
			runLog(run).Warn("lead action: the run could not be released", "error", saveErr)
		}
		return err
	}
}

func permanent(err error) bool {
	return errors.Is(err, leadaction.ErrForbidden) || errors.Is(err, leadaction.ErrSnapshotLost) || failureOf(err) == leadaction.FailureInvalid
}

func failureOf(err error) leadaction.FailureCode {
	switch {
	case errors.Is(err, leadaction.ErrForbidden), errors.Is(err, customfield.ErrValueForbidden):
		return leadaction.FailureForbidden
	case errors.Is(err, leadaction.ErrSnapshotLost):
		return leadaction.FailureSnapshotLost
	case leadaction.ErrorCode(err) != "", customfield.IsValueRefusal(err), errors.Is(err, customfield.ErrUnknownKey),
		errors.Is(err, lead.ErrLeadOwnerInvalid), errors.Is(err, lead.ErrLeadOwnerOutsideWorkspace), errors.Is(err, lead.ErrLeadOwnerOutOfReach):
		return leadaction.FailureInvalid
	}
	return leadaction.FailureInternal
}

func (s *Service) execute(ctx context.Context, run *leadaction.Run, token string) error {
	actor := Actor{WorkspaceID: run.WorkspaceID, UserID: run.ActorID, IsAdmin: run.IsAdmin}
	defs, err := s.authorize(actor, run.Action, run.Params)
	if err != nil {
		return err
	}
	edit, err := s.editFor(actor, run.Action, run.Params, defs)
	if err != nil {
		return err
	}
	if run.Phase == leadaction.PhaseEdit {
		if err := s.applyEdits(ctx, run, token, edit); err != nil {
			return err
		}
		if !run.NeedsMeta() {
			s.finish(ctx, run, token, "")
			return nil
		}
		run.EnterMeta(s.now())
		if err := s.deps.Runs.Save(ctx, run, token); err != nil {
			return err
		}
	}
	if err := s.applyMeta(ctx, run, token, edit); err != nil {
		return err
	}
	s.finish(ctx, run, token, "")
	return nil
}

func (s *Service) frozenPage(ctx context.Context, run *leadaction.Run) ([]string, error) {
	ids, err := s.deps.Selections.Snapshot(ctx, run.WorkspaceID, run.ID, run.Cursor, leadaction.BatchSize)
	if err != nil || len(ids) > 0 {
		return ids, err
	}
	size, err := s.deps.Selections.SnapshotSize(ctx, run.WorkspaceID, run.ID)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, leadaction.ErrSnapshotLost
	}
	return nil, nil
}

func (s *Service) applyEdits(ctx context.Context, run *leadaction.Run, token string, edit leadaction.Edit) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := s.frozenPage(ctx, run)
		if err != nil || len(ids) == 0 {
			return err
		}
		tally, err := s.deps.Writer.ApplyBatch(ctx, leadaction.BatchWrite{
			WorkspaceID: run.WorkspaceID, ActorID: run.ActorID, LeadIDs: ids, Edit: edit, At: s.now(),
		})
		if err != nil {
			return err
		}
		run.Advance(leadaction.BatchOutcome{Cursor: ids[len(ids)-1], Processed: len(ids), Changed: len(tally.Changed), Skipped: tally.Skipped()}, s.now())
		if err := s.deps.Runs.Save(ctx, run, token); err != nil {
			return err
		}
		if len(tally.Changed) > 0 {
			s.deps.Notifier.LeadsBulkUpdated(run.WorkspaceID, run.ID)
		}
		if len(ids) < leadaction.BatchSize {
			return nil
		}
	}
}

func (s *Service) applyMeta(ctx context.Context, run *leadaction.Run, token string, edit leadaction.Edit) error {
	phone, err := s.metaPhone(run.WorkspaceID, run.Params)
	if errors.Is(err, leadaction.ErrPhoneUnavailable) {
		runLog(run).Warn("lead action: the run keeps its local blocks only", "error", err)
		run.Result.MetaUnavailable = true
		return nil
	}
	if err != nil {
		return err
	}
	blocks := edit.Blocks()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := s.frozenPage(ctx, run)
		if err != nil || len(ids) == 0 {
			return err
		}
		targets, err := s.deps.Writer.BlockTargets(ctx, run.WorkspaceID, ids, blocks)
		if err != nil {
			return err
		}
		for i, target := range targets {
			if err := s.awaitMetaSlot(ctx, run, token); err != nil {
				return err
			}
			applied, failed := 1, 0
			if err := phone.Apply(target.Number, blocks); err != nil {
				runLog(run).Warn("lead action: the Meta block of a lead failed", "lead_id", target.LeadID, "error", err)
				applied, failed = 0, 1
			}
			run.Applied(applied, failed)
			run.Advance(leadaction.BatchOutcome{Cursor: target.LeadID}, s.now())
			if (i+1)%metaSaveEvery == 0 {
				if err := s.deps.Runs.Save(ctx, run, token); err != nil {
					return err
				}
			}
		}
		run.Advance(leadaction.BatchOutcome{Cursor: ids[len(ids)-1]}, s.now())
		if err := s.deps.Runs.Save(ctx, run, token); err != nil {
			return err
		}
		if len(ids) < leadaction.BatchSize {
			return nil
		}
	}
}

func (s *Service) awaitMetaSlot(ctx context.Context, run *leadaction.Run, token string) error {
	for {
		allowed, retryAfter, err := s.deps.MetaLimiter.Allow(run.Params.BusinessPhoneID)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
		if retryAfter > metaInlineWait {
			return s.deferMeta(ctx, run, token, retryAfter)
		}
		if err := s.deps.Sleep(ctx, retryAfter); err != nil {
			return err
		}
	}
}

func (s *Service) deferMeta(ctx context.Context, run *leadaction.Run, token string, retryAfter time.Duration) error {
	run.Defer(s.now().Add(retryAfter), s.now())
	if err := s.deps.Runs.Save(ctx, run, token); err != nil {
		return err
	}
	id := run.ID
	s.deps.After(retryAfter, func() { s.launch(id) })
	return errMetaDeferred
}

func (s *Service) finish(ctx context.Context, run *leadaction.Run, token string, failure leadaction.FailureCode) {
	ctx = context.WithoutCancel(ctx)
	if failure == "" {
		run.Finish(s.now())
	} else {
		run.Fail(failure, s.now())
	}
	if err := s.deps.Runs.Save(ctx, run, token); err != nil {
		runLog(run).Warn("lead action: the run could not be closed", "error", err)
		return
	}
	s.countRun(run)
	runLog(run).Info("lead action: run finished", "status", string(run.Status), "failure", string(run.FailureCode),
		"processed", run.Result.Processed, "changed", run.Result.Changed, "meta_applied", run.Result.MetaApplied, "meta_failed", run.Result.MetaFailed)
	s.dropSnapshot(ctx, run.WorkspaceID, run.ID)
}

func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	stalled, err := s.deps.Runs.FailStalled(ctx, now)
	if err != nil {
		return err
	}
	s.countStalled(stalled)
	ids, err := s.deps.Runs.Claimable(ctx, now, sweepLimit)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.launch(id)
	}
	before := now.Add(-leadaction.SnapshotRetention)
	for i := 0; i < snapshotSweeps; i++ {
		dropped, err := s.deps.Selections.SweepSnapshots(ctx, before, snapshotSweepPage)
		if err != nil {
			return err
		}
		if dropped < snapshotSweepPage {
			break
		}
	}
	return nil
}
