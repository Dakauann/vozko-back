package prometheus

// IncBillingSkipped records an AI usage event that did NOT debit a workspace, so a
// revenue leak (unpriced model, missing usage, permanently dropped event) is
// visible on a dashboard/alert instead of buried in logs.
func (p *PrometheusService) IncBillingSkipped(reason string) {
	if p == nil || p.BillingSkippedTotal == nil {
		return
	}
	p.BillingSkippedTotal.WithLabelValues(safeLabel(reason)).Inc()
}
