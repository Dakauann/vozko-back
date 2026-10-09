package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

type geocodingMetrics struct {
	backlog     *prometheus.GaugeVec
	outcomes    *prometheus.CounterVec
	staleWrites prometheus.Counter
}

func newGeocodingMetrics(reg prometheus.Registerer) *geocodingMetrics {
	m := &geocodingMetrics{
		backlog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "backlog", Help: "Addresses waiting for a position, by status (pending|unavailable). Rising for an hour means the sweeper is not keeping up",
		}, []string{"status"}),
		outcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "outcomes_total", Help: "Addresses settled by the sweeper, by precision of the position, or by status when none was found",
		}, []string{"outcome"}),
		staleWrites: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "stale_writes_total", Help: "Positions not written because the address text or its claim changed while it was geocoded",
		}),
	}
	reg.MustRegister(m.backlog, m.outcomes, m.staleWrites)
	return m
}

var _ metrics.GeocodingMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) SetGeocodingBacklog(status string, n int64) {
	if p == nil || p.geocoding == nil {
		return
	}
	p.geocoding.backlog.WithLabelValues(safeLabel(status)).Set(float64(n))
}

func (p *PrometheusService) AddGeocodingOutcomes(outcome string, n int) {
	if p == nil || p.geocoding == nil || n <= 0 {
		return
	}
	p.geocoding.outcomes.WithLabelValues(safeLabel(outcome)).Add(float64(n))
}

func (p *PrometheusService) AddGeocodingStale(n int) {
	if p == nil || p.geocoding == nil || n <= 0 {
		return
	}
	p.geocoding.staleWrites.Add(float64(n))
}
