package prometheus

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/metrics"
)

func scrape(t *testing.T, svc *PrometheusService) string {
	t.Helper()
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return string(body)
}

func TestWorkflowRunStepAndWebhookMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.WorkflowMetricsRecorder = svc
	recorder.IncWorkflowRunStarted("trigger_webhook")
	recorder.IncWorkflowRunFinished("error")
	recorder.ObserveWorkflowStepDuration("action_http_request", 1200*time.Millisecond)
	recorder.IncWorkflowWebhookDelivery("rejected", "workflow_not_active")
	recorder.IncWorkflowWebhookDelivery("accepted", "")

	body := scrape(t, svc)
	for _, want := range []string{
		`app_workflow_runs_started_total{replica_id="replica-1",trigger="trigger_webhook"} 1`,
		`app_workflow_runs_finished_total{replica_id="replica-1",status="error"} 1`,
		`app_workflow_step_duration_seconds_count{node_type="action_http_request",replica_id="replica-1"} 1`,
		`app_workflow_webhook_deliveries_total{outcome="rejected",reason="workflow_not_active",replica_id="replica-1"} 1`,
		`app_workflow_webhook_deliveries_total{outcome="accepted",reason="_",replica_id="replica-1"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output misses %q", want)
		}
	}
}

func TestAIToolCallMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.AIToolCallMetricsRecorder = svc
	recorder.IncAIToolCall("http_request", "error")
	if body := scrape(t, svc); !strings.Contains(body, `app_ai_tool_calls_total{outcome="error",replica_id="replica-1",tool="http_request"} 1`) {
		t.Fatalf("metrics output misses the tool call counter\n%s", body)
	}
}

func TestWorkflowMetricsTolerateANilService(t *testing.T) {
	var svc *PrometheusService
	svc.IncWorkflowRunStarted("x")
	svc.IncWorkflowRunFinished("x")
	svc.ObserveWorkflowStepDuration("x", time.Second)
	svc.IncWorkflowWebhookDelivery("x", "y")
	svc.IncAIToolCall("x", "y")
}

func TestInboundReplyLookupAndAgentContinuationMetricsAreExposed(t *testing.T) {
	svc := NewPrometheusService("replica-1")
	var recorder metrics.WorkflowMetricsRecorder = svc
	recorder.IncWorkflowReplyLookupFailed()
	recorder.IncWorkflowAgentContinuation("sent")

	body := scrape(t, svc)
	for _, want := range []string{
		`app_workflow_reply_lookup_failures_total{replica_id="replica-1"} 1`,
		`app_workflow_agent_continuations_total{outcome="sent",replica_id="replica-1"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output misses %q", want)
		}
	}

	var none *PrometheusService
	none.IncWorkflowReplyLookupFailed()
	none.IncWorkflowAgentContinuation("x")
}
