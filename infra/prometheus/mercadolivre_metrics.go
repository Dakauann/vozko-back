package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

func newMercadoLivreEvents(reg prometheus.Registerer) *prometheus.CounterVec {
	events := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace, Subsystem: "mercadolivre",
		Name: "events_total", Help: "Mercado Livre channel events by stage (notify, consume, send, token, reconcile, health) and outcome",
	}, []string{"stage", "outcome"})
	reg.MustRegister(events)
	return events
}

var _ metrics.MercadoLivreMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) AddMercadoLivreEvent(stage, outcome string) {
	if p == nil || p.mercadoLivre == nil {
		return
	}
	p.mercadoLivre.WithLabelValues(safeLabel(stage), safeLabel(outcome)).Inc()
}
