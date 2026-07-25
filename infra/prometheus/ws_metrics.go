package prometheus

func (p *PrometheusService) IncWSConnections(endpoint string) {
	if p == nil || p.WSConnections == nil {
		return
	}
	p.WSConnections.WithLabelValues(safeLabel(endpoint)).Inc()
}

func (p *PrometheusService) DecWSConnections(endpoint string) {
	if p == nil || p.WSConnections == nil {
		return
	}
	p.WSConnections.WithLabelValues(safeLabel(endpoint)).Dec()
}