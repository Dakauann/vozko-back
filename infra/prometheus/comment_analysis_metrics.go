package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

// Comment-analysis counters (plan §14). Registered on the shared service so
// they carry the same replica_id as everything else.
type commentAnalysisMetrics struct {
	enqueued     *prometheus.CounterVec
	batches      *prometheus.CounterVec
	items        *prometheus.CounterVec
	tokens       *prometheus.CounterVec
	batchLatency prometheus.Histogram
	pending      *prometheus.GaugeVec
	truncated    prometheus.Counter
	capHits      *prometheus.CounterVec
}

func newAudienceMetrics(reg prometheus.Registerer) *commentAnalysisMetrics {
	m := &commentAnalysisMetrics{
		enqueued: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "enqueued_total", Help: "Comments enqueued for analysis, by source",
		}, []string{"source"}),
		batches: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "batches_total", Help: "Model calls, by model and outcome (ok|length|parse_error|provider_error)",
		}, []string{"model", "outcome"}),
		items: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "items_total", Help: "Comments by reconciliation outcome (analyzed|missing_ref|invalid_ref|bad_labels|failed|skipped|deferred)",
		}, []string{"outcome"}),
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "tokens_total", Help: "Tokens consumed, by model and kind (prompt|completion)",
		}, []string{"model", "kind"}),
		batchLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "batch_latency_seconds", Help: "Model call latency per batch",
			Buckets: []float64{0.5, 1, 2, 4, 8, 15, 30, 60},
		}),
		pending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "pending", Help: "Comments waiting for analysis, by source. Rising monotonically means the flush is not keeping up",
		}, []string{"source"}),
		truncated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "truncated_total", Help: "Comments cut at MaxCommentRunes before classification",
		}),
		capHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "comment_analysis",
			Name: "cap_hit_total", Help: "Cycles stopped by a cap (cycle|daily|balance)",
		}, []string{"cap"}),
	}
	reg.MustRegister(m.enqueued, m.batches, m.items, m.tokens, m.batchLatency, m.pending, m.truncated, m.capHits)
	return m
}

var _ metrics.AudienceMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) IncCommentEnqueued(source string) {
	if p == nil || p.audience == nil {
		return
	}
	p.audience.enqueued.WithLabelValues(safeLabel(source)).Inc()
}

func (p *PrometheusService) IncCommentBatches(model, outcome string) {
	if p == nil || p.audience == nil {
		return
	}
	p.audience.batches.WithLabelValues(safeLabel(model), safeLabel(outcome)).Inc()
}

func (p *PrometheusService) AddCommentItems(outcome string, n int) {
	if p == nil || p.audience == nil || n <= 0 {
		return
	}
	p.audience.items.WithLabelValues(safeLabel(outcome)).Add(float64(n))
}

func (p *PrometheusService) AddCommentTokens(model, kind string, n int) {
	if p == nil || p.audience == nil || n <= 0 {
		return
	}
	p.audience.tokens.WithLabelValues(safeLabel(model), safeLabel(kind)).Add(float64(n))
}

func (p *PrometheusService) ObserveCommentBatchLatency(elapsed time.Duration) {
	if p == nil || p.audience == nil {
		return
	}
	p.audience.batchLatency.Observe(elapsed.Seconds())
}

func (p *PrometheusService) SetCommentPending(source string, n int) {
	if p == nil || p.audience == nil {
		return
	}
	p.audience.pending.WithLabelValues(safeLabel(source)).Set(float64(n))
}

func (p *PrometheusService) AddCommentTruncated(n int) {
	if p == nil || p.audience == nil || n <= 0 {
		return
	}
	p.audience.truncated.Add(float64(n))
}

func (p *PrometheusService) IncCommentCapHit(cap string) {
	if p == nil || p.audience == nil {
		return
	}
	p.audience.capHits.WithLabelValues(safeLabel(cap)).Inc()
}
