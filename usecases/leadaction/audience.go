package leadaction_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/customfield"
	"vozko/domain/leadaction"
	adsuc "vozko/usecases/advertising"
)

func audienceKey(workspaceID, jobID string) string {
	return "leadaction:audience:" + workspaceID + ":" + jobID
}

func (s *Service) startAudience(ctx context.Context, req Request) (Outcome, error) {
	sel, err := adsuc.AudienceSelection(req.Selection)
	if err != nil {
		return Outcome{}, err
	}
	filter, err := adsuc.AudienceFilter(req.Selection)
	if err != nil {
		return Outcome{}, err
	}
	defs, err := s.deps.Definitions.ListByObject(req.Actor.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return Outcome{}, fmt.Errorf("lead fields of workspace %s: %w", req.Actor.WorkspaceID, err)
	}
	if err := adsuc.RefuseSensitiveAudience(filter, defs); err != nil {
		return Outcome{}, err
	}
	now := s.now()
	job := leadaction.AudienceJob{
		ID:          derivedID("audience", req.Actor.WorkspaceID, req.Actor.UserID, req.IdempotencyKey),
		WorkspaceID: req.Actor.WorkspaceID, ActorID: req.Actor.UserID,
		Fingerprint: leadaction.RequestFingerprint(req.Action, req.Params, req.Selection),
		Status:      leadaction.AudiencePending, StartedAt: now, UpdatedAt: now,
	}
	key := audienceKey(job.WorkspaceID, job.ID)
	raw, err := json.Marshal(job)
	if err != nil {
		return Outcome{}, err
	}
	reserved, err := s.deps.State.SetNX(key, string(raw), leadaction.AudienceRetention)
	if err != nil {
		return Outcome{}, err
	}
	if !reserved {
		stored, err := s.storedAudience(key)
		if err != nil {
			return Outcome{}, err
		}
		if stored.ActorID != job.ActorID || stored.Fingerprint != job.Fingerprint {
			return Outcome{}, leadaction.ErrIdempotencyKeyReused
		}
		return Outcome{Audience: stored}, nil
	}
	err = s.confirmed(ctx, req, func(ctx context.Context, _ int) error {
		frozen, err := s.deps.Selections.Freeze(ctx, scopeOf(req.Actor, req.DepartmentID), sel, job.ID, nil)
		if err != nil {
			return err
		}
		if frozen.Size == 0 {
			return leadaction.ErrSelectionEmpty
		}
		return nil
	})
	if err != nil {
		if delErr := s.deps.State.Del(key); delErr != nil {
			actionLog(job.ID, job.WorkspaceID, leadaction.ActionMetaAudience).Warn("lead action: the audience key stays until it expires", "error", delErr)
		}
		return Outcome{}, err
	}
	draft := advertising.CustomerListDraft{
		AdAccountID: req.Params.AdAccountID, Name: req.Params.Name, Description: req.Params.Description,
		Source: advertising.SourceCRM, CRMFilter: filter, CRMSnapshotID: job.ID,
	}
	requester := adsuc.Requester{WorkspaceID: req.Actor.WorkspaceID, UserID: req.Actor.UserID, IsAdmin: req.Actor.IsAdmin}
	s.deps.Background(func() { s.buildAudience(job, requester, draft) })
	return Outcome{Audience: &job}, nil
}

func (s *Service) buildAudience(job leadaction.AudienceJob, requester adsuc.Requester, draft advertising.CustomerListDraft) {
	ctx := context.Background()
	logger := actionLog(job.ID, job.WorkspaceID, leadaction.ActionMetaAudience)
	result, err := s.deps.Audiences.CreateCustomerList(ctx, requester, draft)
	if err != nil {
		logger.Warn("lead action: the audience failed", "error", err)
		job.Fail(audienceFailure(err), s.now())
	} else {
		job.Succeed(result.Audience, result.Matched, result.Skipped, s.now())
		logger.Info("lead action: audience built", "matched", job.Matched, "skipped", job.Skipped)
	}
	s.countAudience(job)
	s.dropSnapshot(ctx, job.WorkspaceID, job.ID)
	raw, err := json.Marshal(job)
	if err != nil {
		logger.Warn("lead action: the audience could not be kept", "error", err)
		return
	}
	if err := s.deps.State.SetString(audienceKey(job.WorkspaceID, job.ID), string(raw), leadaction.AudienceRetention); err != nil {
		logger.Warn("lead action: the outcome of the audience was not kept", "error", err)
	}
}

func audienceFailure(err error) string {
	if code := advertising.AudienceErrorCode(err); code != "" {
		return code
	}
	return failureCodeOf(err)
}

func (s *Service) storedAudience(key string) (*leadaction.AudienceJob, error) {
	raw, err := s.deps.State.GetString(key)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, leadaction.ErrAudienceNotFound
	}
	var job leadaction.AudienceJob
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return nil, fmt.Errorf("lead action audience job: %w", err)
	}
	effective := job.Effective(s.now())
	return &effective, nil
}

func (s *Service) Audience(_ context.Context, a Actor, id string) (*leadaction.AudienceJob, error) {
	id = strings.TrimSpace(id)
	if strings.TrimSpace(a.WorkspaceID) == "" || id == "" {
		return nil, leadaction.ErrAudienceNotFound
	}
	job, err := s.storedAudience(audienceKey(a.WorkspaceID, id))
	if err != nil {
		return nil, err
	}
	if !job.VisibleTo(a.UserID) || job.WorkspaceID != a.WorkspaceID {
		return nil, leadaction.ErrAudienceNotFound
	}
	return job, nil
}
