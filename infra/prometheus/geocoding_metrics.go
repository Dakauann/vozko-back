package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

type geocodingMetrics struct {
	backlog         *prometheus.GaugeVec
	outcomes        *prometheus.CounterVec
	providerLatency *prometheus.HistogramVec
	providerErrors  *prometheus.CounterVec
	quotaHits       *prometheus.CounterVec
	staleWrites     prometheus.Counter
	providerPauses  *prometheus.CounterVec
	pausedUntil     *prometheus.GaugeVec
	answersReused   prometheus.Counter
}

func newGeocodingMetrics(reg prometheus.Registerer) *geocodingMetrics {
	m := &geocodingMetrics{
		backlog: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "backlog", Help: "Addresses waiting for a position, by status (pending|unavailable|quota_exceeded). Rising for an hour means the sweeper is not keeping up",
		}, []string{"status"}),
		outcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "outcomes_total", Help: "Addresses settled by the sweeper, by precision of the position, or by status when none was found",
		}, []string{"outcome"}),
		providerLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "provider_seconds", Help: "External geocoding provider latency, by provider and result (ok|not_found|ambiguous|unavailable|error)",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		}, []string{"provider", "result"}),
		providerErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "provider_errors_total", Help: "External geocoding calls that failed or answered unavailable, by provider",
		}, []string{"provider"}),
		quotaHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "quota_hits_total", Help: "Workspaces that reached their external geocoding share, by quota (monthly|daily)",
		}, []string{"quota"}),
		staleWrites: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "stale_writes_total", Help: "Positions not written because the address text or its claim changed while it was geocoded",
		}),
		providerPauses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "provider_pauses_total", Help: "Pauses opened for every workspace because the provider refused the account (key rejected, key disabled, account quota spent), by provider and reason. Any increase needs an operator",
		}, []string{"provider", "reason"}),
		pausedUntil: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "provider_paused_until_seconds", Help: "Unix time at which the provider pause seen by this replica ends, by provider and reason. External geocoding is paused for every workspace while it is in the future",
		}, []string{"provider", "reason"}),
		answersReused: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "geocoding",
			Name: "answers_reused_total", Help: "Addresses answered from the workspace's stored provider answers, with no usage slot and no provider call",
		}),
	}
	reg.MustRegister(m.backlog, m.outcomes, m.providerLatency, m.providerErrors, m.quotaHits, m.staleWrites, m.providerPauses, m.pausedUntil, m.answersReused)
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

func (p *PrometheusService) ObserveGeocodingProvider(provider, result string, elapsed time.Duration) {
	if p == nil || p.geocoding == nil {
		return
	}
	p.geocoding.providerLatency.WithLabelValues(safeLabel(provider), safeLabel(result)).Observe(elapsed.Seconds())
	if result == metrics.GeocodingProviderError || result == metrics.GeocodingProviderUnavailable {
		p.geocoding.providerErrors.WithLabelValues(safeLabel(provider)).Inc()
	}
}

func (p *PrometheusService) IncGeocodingQuotaHit(quota string) {
	if p == nil || p.geocoding == nil {
		return
	}
	p.geocoding.quotaHits.WithLabelValues(safeLabel(quota)).Inc()
}

func (p *PrometheusService) AddGeocodingStale(n int) {
	if p == nil || p.geocoding == nil || n <= 0 {
		return
	}
	p.geocoding.staleWrites.Add(float64(n))
}

func (p *PrometheusService) IncGeocodingProviderPause(provider, reason string) {
	if p == nil || p.geocoding == nil {
		return
	}
	p.geocoding.providerPauses.WithLabelValues(safeLabel(provider), safeLabel(reason)).Inc()
}

func (p *PrometheusService) IncGeocodingAnswerReused() {
	if p == nil || p.geocoding == nil {
		return
	}
	p.geocoding.answersReused.Inc()
}

func (p *PrometheusService) SetGeocodingProviderPausedUntil(provider, reason string, until time.Time) {
	if p == nil || p.geocoding == nil {
		return
	}
	p.geocoding.pausedUntil.WithLabelValues(safeLabel(provider), safeLabel(reason)).Set(float64(until.Unix()))
}
