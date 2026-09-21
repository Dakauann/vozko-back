package prometheus

func (p *PrometheusService) IncBillingSkipped(reason string) {
	if p == nil || p.BillingSkippedTotal == nil {
		return
	}
	p.BillingSkippedTotal.WithLabelValues(safeLabel(reason)).Inc()
}
