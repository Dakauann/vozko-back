package leadsend_usecase

import (
	"log/slog"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/leadaction"
	"vozko/domain/metrics"
)

func RunLog(runID, workspaceID string, action leadaction.Action) *slog.Logger {
	return slog.With("run_id", runID, "workspace_id", workspaceID, "action", string(action))
}

func (s *Service) countSend(action leadaction.Action, status string, began time.Time) {
	s.deps.Metrics.AddLeadActionRuns(string(action), status, metrics.LeadActionNoFailure, 1)
	s.deps.Metrics.ObserveLeadActionDuration(string(action), status, s.now().Sub(began))
}

func (s *Service) countPrepared(action leadaction.Action, review *campaign.SendReview, began time.Time) {
	s.countSend(action, metrics.LeadActionPrepared, began)
	for reason, n := range review.Skipped {
		s.deps.Metrics.AddLeadActionSkips(string(action), string(reason), n)
	}
}
