package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"vozko/domain/metrics"
)

type workflowMetrics struct {
	runsStarted  *prometheus.CounterVec
	runsFinished *prometheus.CounterVec
	stepDuration *prometheus.HistogramVec
	deliveries   *prometheus.CounterVec
	toolCalls    *prometheus.CounterVec

	replyLookupFailures prometheus.Counter
	continuations       *prometheus.CounterVec
	choiceWrites        *prometheus.CounterVec
}

func newWorkflowMetrics(reg prometheus.Registerer) *workflowMetrics {
	m := &workflowMetrics{
		runsStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "workflow",
			Name: "runs_started_total", Help: "Workflow runs that executed their first step, by trigger type",
		}, []string{"trigger"}),
		runsFinished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "workflow",
			Name: "runs_finished_total", Help: "Workflow runs that reached a terminal status (completed|error|cancelled)",
		}, []string{"status"}),
		stepDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Subsystem: "workflow",
			Name: "step_duration_seconds", Help: "Time a node executor took per step, by node type",
			Buckets: []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"node_type"}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "workflow",
			Name: "webhook_deliveries_total", Help: "Calls to workflow webhooks, by outcome (accepted|duplicate|already_running|rejected) and rejection reason",
		}, []string{"outcome", "reason"}),
		toolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "ai",
			Name: "tool_calls_total", Help: "Tool calls made by AI agents in conversations, by tool and outcome (ok|error|requested)",
		}, []string{"tool", "outcome"}),
		replyLookupFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "workflow",
			Name: "reply_lookup_failures_total", Help: "Inbound messages left without an automated reply because the waiting-run lookup failed",
		}),
		continuations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "workflow",
			Name: "agent_continuations_total", Help: "Agent turns after an agent-started run ended, by outcome (sent|skipped reason|failed)",
		}, []string{"outcome"}),
		choiceWrites: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Subsystem: "choice_stats",
			Name: "writes_total", Help: "Choice click-through writes by operation (send|click|link_click) and outcome (recorded|duplicate|skipped|failed|zone_unknown)",
		}, []string{"op", "outcome"}),
	}
	reg.MustRegister(m.runsStarted, m.runsFinished, m.stepDuration, m.deliveries, m.toolCalls, m.replyLookupFailures, m.continuations, m.choiceWrites)
	return m
}

func (p *PrometheusService) IncWorkflowReplyLookupFailed() {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.replyLookupFailures.Inc()
}

func (p *PrometheusService) IncWorkflowAgentContinuation(outcome string) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.continuations.WithLabelValues(safeLabel(outcome)).Inc()
}

func (p *PrometheusService) IncChoiceStatsWrite(op, outcome string) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.choiceWrites.WithLabelValues(safeLabel(op), safeLabel(outcome)).Inc()
}

var (
	_ metrics.WorkflowMetricsRecorder    = (*PrometheusService)(nil)
	_ metrics.AIToolCallMetricsRecorder  = (*PrometheusService)(nil)
	_ metrics.ChoiceStatsMetricsRecorder = (*PrometheusService)(nil)
)

func (p *PrometheusService) IncWorkflowRunStarted(trigger string) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.runsStarted.WithLabelValues(safeLabel(trigger)).Inc()
}

func (p *PrometheusService) IncWorkflowRunFinished(status string) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.runsFinished.WithLabelValues(safeLabel(status)).Inc()
}

func (p *PrometheusService) ObserveWorkflowStepDuration(nodeType string, elapsed time.Duration) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.stepDuration.WithLabelValues(safeLabel(nodeType)).Observe(elapsed.Seconds())
}

func (p *PrometheusService) IncWorkflowWebhookDelivery(outcome, reason string) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.deliveries.WithLabelValues(safeLabel(outcome), safeLabel(reason)).Inc()
}

func (p *PrometheusService) IncAIToolCall(tool, outcome string) {
	if p == nil || p.workflow == nil {
		return
	}
	p.workflow.toolCalls.WithLabelValues(safeLabel(tool), safeLabel(outcome)).Inc()
}
