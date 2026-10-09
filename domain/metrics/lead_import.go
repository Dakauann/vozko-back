package metrics

import "time"

const (
	LeadImportRowsCreated   = "created"
	LeadImportRowsEnriched  = "enriched"
	LeadImportRowsUnchanged = "unchanged"
	LeadImportRowsSkipped   = "skipped"
	LeadImportRowsRejected  = "rejected"

	LeadImportDone = "done"
)

type LeadImportMetricsRecorder interface {
	AddLeadImportRows(outcome string, n int)
	AddLeadImportRuns(outcome string, n int)
	ObserveLeadImportDuration(elapsed time.Duration)
}
