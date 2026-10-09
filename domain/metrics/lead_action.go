package metrics

import "time"

const (
	LeadActionDone     = "done"
	LeadActionFailed   = "failed"
	LeadActionCreated  = "created"
	LeadActionPrepared = "prepared"
	LeadActionStarted  = "started"

	LeadActionNoFailure = "none"

	LeadActionSkipMetaFailed = "meta_failed"
	LeadActionSkipNoMatchKey = "no_match_key"
)

type LeadActionMetricsRecorder interface {
	AddLeadActionRuns(action, status, failure string, n int)
	ObserveLeadActionDuration(action, status string, elapsed time.Duration)
	AddLeadActionSkips(action, reason string, n int)
}
