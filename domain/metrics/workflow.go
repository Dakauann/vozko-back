package metrics

import "time"

type WorkflowMetricsRecorder interface {
	IncWorkflowRunStarted(trigger string)
	IncWorkflowRunFinished(status string)
	ObserveWorkflowStepDuration(nodeType string, elapsed time.Duration)
	IncWorkflowWebhookDelivery(outcome, reason string)
	IncWorkflowReplyLookupFailed()
	IncWorkflowAgentContinuation(outcome string)
}

type AIToolCallMetricsRecorder interface {
	IncAIToolCall(tool, outcome string)
}
