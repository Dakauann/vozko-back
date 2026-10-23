package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

type googleSheetsMetrics struct {
	events    *prometheus.CounterVec
	batchRows prometheus.Histogram
	batchTime prometheus.Histogram
}

func newGoogleSheetsMetrics(reg prometheus.Registerer) googleSheetsMetrics {
	m := googleSheetsMetrics{
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "google_sheets",
			Name: "events_total", Help: "Google Sheets row pipeline events by stage (accept, write, header, recover, limiter) and outcome",
		}, []string{"stage", "outcome"}),
		batchRows: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "google_sheets",
			Name: "batch_rows", Help: "Rows sent in one Google Sheets append",
			Buckets: []float64{1, 2, 5, 10, 25, 50, 100, 250, 500},
		}),
		batchTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "google_sheets",
			Name: "batch_seconds", Help: "Duration of one Google Sheets append",
			Buckets: prometheus.DefBuckets,
		}),
	}
	reg.MustRegister(m.events, m.batchRows, m.batchTime)
	return m
}

var _ metrics.GoogleSheetsMetricsRecorder = (*PrometheusService)(nil)

func (p *PrometheusService) AddGoogleSheetsEvent(stage, outcome string) {
	if p == nil || p.googleSheets.events == nil {
		return
	}
	p.googleSheets.events.WithLabelValues(safeLabel(stage), safeLabel(outcome)).Inc()
}

func (p *PrometheusService) ObserveGoogleSheetsBatch(rows int, seconds float64) {
	if p == nil || p.googleSheets.batchRows == nil {
		return
	}
	p.googleSheets.batchRows.Observe(float64(rows))
	p.googleSheets.batchTime.Observe(seconds)
}
