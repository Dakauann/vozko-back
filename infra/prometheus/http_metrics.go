package prometheus

import "time"

func (p *PrometheusService) ObserveHTTPLatency(method, path, status string, elapsed time.Duration) {
	if p == nil || p.HTTPLatency == nil {
		return
	}
	if elapsed < 0 {
		elapsed = 0
	}
	p.HTTPLatency.WithLabelValues(method, path, status).Observe(elapsed.Seconds())
}

func (p *PrometheusService) IncHTTPRequests(method, path, status string) {
	if p == nil || p.HTTPRequestsTotal == nil {
		return
	}
	p.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
}

func (p *PrometheusService) IncHTTPInFlight(method, path string) {
	if p == nil || p.HTTPInFlight == nil {
		return
	}
	p.HTTPInFlight.WithLabelValues(method, path).Inc()
}

func (p *PrometheusService) DecHTTPInFlight(method, path string) {
	if p == nil || p.HTTPInFlight == nil {
		return
	}
	p.HTTPInFlight.WithLabelValues(method, path).Dec()
}
