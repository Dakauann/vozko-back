package leadaction_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/leadaction"
	"vozko/domain/selection"
	adsuc "vozko/usecases/advertising"
)

func previewKey(workspaceID, id string) string {
	return "leadaction:preview:" + workspaceID + ":" + id
}

type previewSource func(ctx context.Context, after string) ([]string, error)

func (s *Service) Preview(ctx context.Context, req Request) (*leadaction.Preview, error) {
	req.Params = req.Params.Normalized()
	defs, err := s.authorize(req.Actor, req.Action, req.Params)
	if err != nil {
		return nil, err
	}
	if err := req.Selection.ValidateForCount(); err != nil {
		return nil, err
	}
	if err := s.checkPhone(req.Actor, req.Action, req.Params); err != nil {
		return nil, err
	}
	if err := s.checkCallList(ctx, req); err != nil {
		return nil, err
	}
	if req.Action.Sends() {
		return s.previewSend(ctx, req)
	}
	var edit *leadaction.Edit
	if req.Action.Runs() {
		e, err := s.editFor(req.Actor, req.Action, req.Params, defs)
		if err != nil {
			return nil, err
		}
		edit = &e
	}
	counted := req.Selection
	if req.Action == leadaction.ActionMetaAudience {
		if counted, err = s.audiencePreviewSelection(req); err != nil {
			return nil, err
		}
	}
	p := &leadaction.Preview{
		ID: s.deps.NewID(), WorkspaceID: req.Actor.WorkspaceID, ActorID: req.Actor.UserID, Action: req.Action,
		Status: leadaction.PreviewRunning, UpdatedAt: s.now(),
		Result: leadaction.PreviewResult{Fingerprint: selection.Fingerprint(req.Selection.EffectiveFilter()), Skipped: map[leadaction.SkipReason]int{}},
	}
	scope := scopeOf(req.Actor, req.DepartmentID)
	deadline := s.now().Add(leadaction.PreviewBudget)
	err = s.gated(ctx, func(ctx context.Context) error {
		matched, err := s.deps.Selections.Count(ctx, scope, req.Selection.BeforeExclusions())
		if err != nil {
			return err
		}
		p.Result.Matched, p.Result.ExpectedCount = matched, req.Selection.ConfirmableCount(matched)
		if edit != nil {
			return nil
		}
		selected, err := s.deps.Selections.Selected(ctx, scope, counted)
		p.Result.Selected, p.Result.Eligible = selected, selected
		return err
	})
	if err != nil {
		return nil, err
	}
	if edit == nil {
		p.Status = leadaction.PreviewDone
		return p, nil
	}
	source, release, err := s.previewSource(ctx, scope, req.Selection, *edit, p.ID)
	if err != nil {
		return nil, err
	}
	finished, err := s.tally(ctx, source, scope.WorkspaceID, *edit, p, deadline)
	if err != nil {
		release()
		return nil, err
	}
	if finished {
		release()
		p.Status = leadaction.PreviewDone
		return p, nil
	}
	if err := s.keepPreview(p); err != nil {
		release()
		return nil, err
	}
	background := p.Clone()
	s.deps.Background(func() {
		defer release()
		s.finishPreview(source, scope.WorkspaceID, *edit, background)
	})
	return p, nil
}

func (s *Service) audiencePreviewSelection(req Request) (selection.Selection, error) {
	sel, err := adsuc.AudienceSelection(req.Selection)
	if err != nil {
		return selection.Selection{}, err
	}
	filter, err := adsuc.AudienceFilter(req.Selection)
	if err != nil {
		return selection.Selection{}, err
	}
	defs, err := s.deps.Definitions.ListByObject(req.Actor.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return selection.Selection{}, fmt.Errorf("lead fields of workspace %s: %w", req.Actor.WorkspaceID, err)
	}
	return sel, adsuc.RefuseSensitiveAudience(filter, defs)
}

func (s *Service) previewSource(ctx context.Context, scope selection.Scope, sel selection.Selection, edit leadaction.Edit, previewID string) (previewSource, func(), error) {
	if sel.Mode != selection.ModeFirstN {
		return func(ctx context.Context, after string) ([]string, error) {
			var refs []selection.Ref
			err := s.gated(ctx, func(ctx context.Context) error {
				var err error
				refs, err = s.deps.Selections.Resolve(ctx, scope, sel, after, leadaction.PreviewChunk)
				return err
			})
			ids := make([]string, 0, len(refs))
			for _, ref := range refs {
				ids = append(ids, ref.ID)
			}
			return ids, err
		}, func() {}, nil
	}
	err := s.gated(ctx, func(ctx context.Context) error {
		_, err := s.deps.Selections.Freeze(ctx, scope, sel, previewID, edit.PendingFor(sel))
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	release := func() { s.dropSnapshot(ctx, scope.WorkspaceID, previewID) }
	return func(ctx context.Context, after string) ([]string, error) {
		return s.deps.Selections.Snapshot(ctx, scope.WorkspaceID, previewID, after, leadaction.PreviewChunk)
	}, release, nil
}

func (s *Service) tally(ctx context.Context, source previewSource, workspaceID string, edit leadaction.Edit, p *leadaction.Preview, deadline time.Time) (bool, error) {
	for {
		ids, err := source(ctx, p.Cursor)
		if err != nil {
			return false, err
		}
		if len(ids) == 0 {
			return true, nil
		}
		t, err := s.deps.Writer.TallyBatch(ctx, workspaceID, ids, edit)
		if err != nil {
			return false, err
		}
		p.Result.Add(len(ids), t)
		p.Cursor = ids[len(ids)-1]
		p.UpdatedAt = s.now()
		if len(ids) < leadaction.PreviewChunk {
			return true, nil
		}
		if !deadline.IsZero() && p.UpdatedAt.After(deadline) {
			return false, nil
		}
	}
}

func (s *Service) finishPreview(source previewSource, workspaceID string, edit leadaction.Edit, p *leadaction.Preview) {
	ctx := context.Background()
	if _, err := s.tally(ctx, source, workspaceID, edit, p, time.Time{}); err != nil {
		previewLog(p).Warn("lead action: the preview failed", "error", err)
		p.Status, p.FailureCode = leadaction.PreviewFailed, failureCodeOf(err)
	} else {
		p.Status = leadaction.PreviewDone
	}
	p.UpdatedAt = s.now()
	if err := s.keepPreview(p); err != nil {
		previewLog(p).Warn("lead action: the preview could not be kept", "error", err)
	}
}

func failureCodeOf(err error) string {
	if code := selection.ErrorCode(err); code != "" {
		return code
	}
	if code := leadaction.ErrorCode(err); code != "" {
		return code
	}
	return string(leadaction.FailureInternal)
}

func (s *Service) keepPreview(p *leadaction.Preview) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.deps.State.SetString(previewKey(p.WorkspaceID, p.ID), string(raw), leadaction.PreviewRetention)
}

func (s *Service) PreviewStatus(_ context.Context, a Actor, id string) (*leadaction.Preview, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" || strings.TrimSpace(id) == "" {
		return nil, leadaction.ErrPreviewNotFound
	}
	raw, err := s.deps.State.GetString(previewKey(a.WorkspaceID, strings.TrimSpace(id)))
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, leadaction.ErrPreviewNotFound
	}
	var p leadaction.Preview
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("lead action preview %s: %w", id, err)
	}
	if !p.VisibleTo(a.UserID) || p.WorkspaceID != a.WorkspaceID {
		return nil, leadaction.ErrPreviewNotFound
	}
	return &p, nil
}
