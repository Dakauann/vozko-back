package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

type leadActionMetrics struct {
	runs     *prometheus.CounterVec
	duration *prometheus.HistogramVec
	skips    *prometheus.CounterVec
}

func newLeadActionMetrics(reg prometheus.Registerer) *leadActionMetrics {
	m := &leadActionMetrics{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "lead_action",
			Name: "runs_total", Help: "Lead selection actions and sends, by action and status (done|failed for runs and Meta audiences, created for exports and call lists, prepared|started for sends) and failure code (none when it did not fail)",
		}, []string{"action", "status", "failure"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "lead_action",
			Name: "duration_seconds", Help: "Lead action time, by action and status: from the request to the end for runs and Meta audiences, the request itself for exports, call lists and sends",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200},
		}, []string{"action", "status"}),
		skips: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "lead_action",
			Name: "skips_total", Help: "Leads a lead action left out, by action and reason (unchanged|gone|meta_failed for runs, no_match_key for Meta audiences, the campaign skip reason for sends)",
		}, []string{"action", "reason"}),
	}
	reg.MustRegister(m.runs, m.duration, m.skips)
	return m
}

var _ metrics.LeadActionMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) AddLeadActionRuns(action, status, failure string, n int) {
	if p == nil || p.leadAction == nil || n <= 0 {
		return
	}
	p.leadAction.runs.WithLabelValues(safeLabel(action), safeLabel(status), safeLabel(failure)).Add(float64(n))
}

func (p *PrometheusService) ObserveLeadActionDuration(action, status string, elapsed time.Duration) {
	if p == nil || p.leadAction == nil {
		return
	}
	p.leadAction.duration.WithLabelValues(safeLabel(action), safeLabel(status)).Observe(elapsed.Seconds())
}

func (p *PrometheusService) AddLeadActionSkips(action, reason string, n int) {
	if p == nil || p.leadAction == nil || n <= 0 {
		return
	}
	p.leadAction.skips.WithLabelValues(safeLabel(action), safeLabel(reason)).Add(float64(n))
}
