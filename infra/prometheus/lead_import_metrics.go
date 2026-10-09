package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

type leadImportMetrics struct {
	rows     *prometheus.CounterVec
	runs     *prometheus.CounterVec
	duration prometheus.Histogram
}

func newLeadImportMetrics(reg prometheus.Registerer) *leadImportMetrics {
	m := &leadImportMetrics{
		rows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "lead_import",
			Name: "rows_total", Help: "Sheet rows written by lead imports, by outcome (created|enriched|unchanged|skipped|rejected). Dry runs are not counted",
		}, []string{"outcome"}),
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "lead_import",
			Name: "runs_total", Help: "Lead imports that stopped, by outcome: done, or the failure code (stalled|file_unavailable|forbidden|internal|interrupted)",
		}, []string{"outcome"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "lead_import",
			Name: "duration_seconds", Help: "Time from start to done of a lead import, rows, family links and inbox seed included",
			Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1200, 2400, 3600},
		}),
	}
	reg.MustRegister(m.rows, m.runs, m.duration)
	return m
}

var _ metrics.LeadImportMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) AddLeadImportRows(outcome string, n int) {
	if p == nil || p.leadImport == nil || n <= 0 {
		return
	}
	p.leadImport.rows.WithLabelValues(safeLabel(outcome)).Add(float64(n))
}

func (p *PrometheusService) AddLeadImportRuns(outcome string, n int) {
	if p == nil || p.leadImport == nil || n <= 0 {
		return
	}
	p.leadImport.runs.WithLabelValues(safeLabel(outcome)).Add(float64(n))
}

func (p *PrometheusService) ObserveLeadImportDuration(elapsed time.Duration) {
	if p == nil || p.leadImport == nil {
		return
	}
	p.leadImport.duration.Observe(elapsed.Seconds())
}
