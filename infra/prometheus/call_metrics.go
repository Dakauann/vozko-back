package prometheus

func (p *PrometheusService) IncCallDialing() {
	if p == nil || p.CallStates == nil {
		return
	}
	p.CallStates.WithLabelValues("dialing").Inc()
}

func (p *PrometheusService) DecCallDialing() {
	if p == nil || p.CallStates == nil {
		return
	}
	p.CallStates.WithLabelValues("dialing").Dec()
}

func (p *PrometheusService) IncCallOngoing() {
	if p == nil || p.CallStates == nil {
		return
	}
	p.CallStates.WithLabelValues("ongoing").Inc()
}

func (p *PrometheusService) DecCallOngoing() {
	if p == nil || p.CallStates == nil {
		return
	}
	p.CallStates.WithLabelValues("ongoing").Dec()
}

func (p *PrometheusService) IncCallFinished() {
	if p == nil || p.FinishedCalls == nil {
		return
	}
	p.FinishedCalls.Inc()
}
