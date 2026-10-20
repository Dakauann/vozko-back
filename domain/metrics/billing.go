package metrics

type BillingMetricsRecorder interface {
	IncBillingSkipped(reason string)
}
