package prometheus

import (
	"net/http"

	"vozko/domain/calls"
	"vozko/domain/metrics"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsNamespace is the fixed, brand-neutral prefix for all app metrics
// (e.g. app_http_requests_total). It is intentionally NOT the brand key: metric
// names must stay stable across brands so one set of dashboards/alert rules works
// for every deployment. Per-brand distinction, if ever needed, belongs in a label.
const metricsNamespace = "app"

type PrometheusService struct {
	HTTPRequestsTotal *prometheus.CounterVec
	HTTPLatency       *prometheus.HistogramVec
	HTTPInFlight      *prometheus.GaugeVec

	CallStates    *prometheus.GaugeVec
	FinishedCalls prometheus.Counter

	EmailSendErrors     *prometheus.CounterVec
	MetricsRecordErrors *prometheus.CounterVec

	LoopGuardCheckedTotal *prometheus.CounterVec
	LoopGuardBlockedTotal *prometheus.CounterVec

	RateLimited *prometheus.CounterVec

	BillingSkippedTotal *prometheus.CounterVec

	WSConnections *prometheus.GaugeVec

	registry *prometheus.Registry
}

func NewPrometheusService(replicaID string) *PrometheusService {
	registry := prometheus.NewRegistry()

	reg := prometheus.WrapRegistererWith(prometheus.Labels{"replica_id": replicaID}, registry)

	httpRequestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "http",
			Name: "requests_total", Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)
	httpLatency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "http",
			Name: "request_duration_seconds", Help: "HTTP request latency in seconds, by endpoint",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path", "status"},
	)
	httpInFlight := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "http",
			Name: "in_flight_requests", Help: "Number of HTTP requests currently in flight",
		},
		[]string{"method", "path"},
	)

	callStates := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "calls",
			Name: "current", Help: "Current number of calls by state",
		},
		[]string{"state"},
	)
	finishedCalls := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "calls",
			Name: "finished_total", Help: "Total number of finished calls",
		},
	)

	emailSendErrors := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "email",
			Name: "send_errors_total", Help: "Total number of email sending errors",
		},
		[]string{"error_type"},
	)
	metricsRecordErrors := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "metrics",
			Name: "record_errors_total", Help: "Total number of metric recording errors",
		},
		[]string{"event_type", "error_type"},
	)

	loopGuardChecked := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "loopguard",
			Name: "checks_total", Help: "Total conversation loop-guard checks evaluated",
		},
		[]string{"layer", "action"},
	)
	loopGuardBlocked := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "loopguard",
			Name: "blocks_total", Help: "Total times the conversation loop-guard blocked an AI response",
		},
		[]string{"reason"},
	)

	billingSkipped := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "billing",
			Name: "skipped_total", Help: "Total AI billing events that did NOT debit a workspace, by reason (potential revenue leak)",
		},
		[]string{"reason"},
	)

	// client_ip is labelled ONLY here, on the rejection path. Cardinality stays
	// bounded to IPs that actually hit a limit — a single IP with a large count
	// means many users behind one NAT (e.g. an office) sharing one per-IP budget.
	rateLimited := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "http",
			Name: "rate_limited_total", Help: "Total requests rejected by a per-IP rate limiter, by limiter, client IP and reason",
		},
		[]string{"limiter", "client_ip", "reason"},
	)

	wsConnections := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace, Subsystem: "ws",
			Name: "connections", Help: "Number of currently connected WebSocket clients by endpoint",
		},
		[]string{"endpoint"},
	)

	reg.MustRegister(
		httpRequestsTotal, httpLatency, httpInFlight,
		callStates, finishedCalls, emailSendErrors, metricsRecordErrors,
		loopGuardChecked, loopGuardBlocked,
		billingSkipped,
		wsConnections,
		rateLimited,
	)

	callStates.WithLabelValues("dialing").Set(0)
	callStates.WithLabelValues("ongoing").Set(0)
	wsConnections.WithLabelValues(metrics.WSEndpointConversations).Set(0)
	wsConnections.WithLabelValues(metrics.WSEndpointDialer).Set(0)
	wsConnections.WithLabelValues(metrics.WSEndpointWorkflowSimulator).Set(0)

	// Register the Go runtime and process collectors through the replica_id-wrapped
	// registerer (reg), not the raw registry, so go_* and process_* series carry the
	// same replica_id label as our app metrics. Without this they only get the
	// scrape-time `instance` label (host:port), which makes the resources dashboard
	// host-scoped ("localhost:9213") instead of replica-scoped.
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return &PrometheusService{
		HTTPRequestsTotal:     httpRequestsTotal,
		HTTPLatency:           httpLatency,
		HTTPInFlight:          httpInFlight,
		CallStates:            callStates,
		FinishedCalls:         finishedCalls,
		EmailSendErrors:       emailSendErrors,
		MetricsRecordErrors:   metricsRecordErrors,
		LoopGuardCheckedTotal: loopGuardChecked,
		LoopGuardBlockedTotal: loopGuardBlocked,
		BillingSkippedTotal:   billingSkipped,
		WSConnections:         wsConnections,
		RateLimited:           rateLimited,
		registry:              registry,
	}
}

func (p *PrometheusService) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{})
}

func (p *PrometheusService) Registry() *prometheus.Registry {
	return p.registry
}

func safeLabel(s string) string {
	if s == "" {
		return "_"
	}
	return s
}

func (p *PrometheusService) IncEmailSendError(errorType string) {
	if p == nil || p.EmailSendErrors == nil {
		return
	}
	p.EmailSendErrors.WithLabelValues(errorType).Inc()
}

func (p *PrometheusService) IncMetricsRecordError(eventType, errorType string) {
	if p == nil || p.MetricsRecordErrors == nil {
		return
	}
	p.MetricsRecordErrors.WithLabelValues(eventType, errorType).Inc()
}

func (p *PrometheusService) IncRateLimited(limiter, clientIP, reason string) {
	if p == nil || p.RateLimited == nil {
		return
	}
	p.RateLimited.WithLabelValues(safeLabel(limiter), safeLabel(clientIP), safeLabel(reason)).Inc()
}

var (
	_ metrics.MetricsService           = &PrometheusService{}
	_ metrics.HTTPMetricsRecorder      = &PrometheusService{}
	_ metrics.WSMetricsRecorder        = &PrometheusService{}
	_ metrics.RateLimitMetricsRecorder = &PrometheusService{}
	_ calls.CallMetricsRecorder        = &PrometheusService{}
)
