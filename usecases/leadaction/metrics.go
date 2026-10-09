package leadaction_usecase

import (
	"log/slog"
	"time"

	"vozko/domain/leadaction"
	"vozko/domain/metrics"
	leadsend_usecase "vozko/usecases/leadsend"
)

var actionLog = leadsend_usecase.RunLog

func runLog(run *leadaction.Run) *slog.Logger {
	return actionLog(run.ID, run.WorkspaceID, run.Action)
}

func previewLog(p *leadaction.Preview) *slog.Logger {
	return slog.With("preview_id", p.ID, "workspace_id", p.WorkspaceID, "action", string(p.Action))
}

func (s *Service) countRun(run *leadaction.Run) {
	action := string(run.Action)
	status, failure := metrics.LeadActionDone, metrics.LeadActionNoFailure
	if run.Status == leadaction.StatusFailed {
		status, failure = metrics.LeadActionFailed, string(run.FailureCode)
	}
	s.deps.Metrics.AddLeadActionRuns(action, status, failure, 1)
	if run.FinishedAt != nil {
		s.deps.Metrics.ObserveLeadActionDuration(action, status, run.FinishedAt.Sub(run.CreatedAt))
	}
	for reason, n := range run.Result.Skipped {
		s.deps.Metrics.AddLeadActionSkips(action, string(reason), n)
	}
	s.deps.Metrics.AddLeadActionSkips(action, metrics.LeadActionSkipMetaFailed, run.Result.MetaFailed)
}

func (s *Service) countAudience(job leadaction.AudienceJob) {
	action := string(leadaction.ActionMetaAudience)
	status, failure := metrics.LeadActionDone, metrics.LeadActionNoFailure
	if job.Status == leadaction.AudienceFailed {
		status, failure = metrics.LeadActionFailed, job.FailureCode
	}
	s.deps.Metrics.AddLeadActionRuns(action, status, failure, 1)
	s.deps.Metrics.ObserveLeadActionDuration(action, status, job.UpdatedAt.Sub(job.StartedAt))
	s.deps.Metrics.AddLeadActionSkips(action, metrics.LeadActionSkipNoMatchKey, job.Skipped)
}

func (s *Service) countCreated(action leadaction.Action, began time.Time) {
	s.deps.Metrics.AddLeadActionRuns(string(action), metrics.LeadActionCreated, metrics.LeadActionNoFailure, 1)
	s.deps.Metrics.ObserveLeadActionDuration(string(action), metrics.LeadActionCreated, s.now().Sub(began))
}

func (s *Service) countStalled(stalled map[leadaction.Action]int) {
	for action, n := range stalled {
		s.deps.Metrics.AddLeadActionRuns(string(action), metrics.LeadActionFailed, string(leadaction.FailureStalled), n)
		slog.Warn("lead action: runs stopped after every attempt", "action", string(action), "runs", n, "attempts", leadaction.MaxAttempts)
	}
}
