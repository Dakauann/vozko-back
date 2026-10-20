package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

func newTikTokEvents(reg prometheus.Registerer) *prometheus.CounterVec {
	events := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace, Subsystem: "tiktok",
		Name: "events_total", Help: "TikTok channel events by stage (webhook, consume, send, token, comments, purge, health) and outcome",
	}, []string{"stage", "outcome"})
	reg.MustRegister(events)
	return events
}

var _ metrics.TikTokMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) AddTikTokEvent(stage, outcome string) {
	if p == nil || p.tiktok == nil {
		return
	}
	p.tiktok.WithLabelValues(safeLabel(stage), safeLabel(outcome)).Inc()
}
