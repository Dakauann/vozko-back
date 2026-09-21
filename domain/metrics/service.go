package metrics

import "time"

type MetricsService interface {
	IncEmailSendError(errorType string)
	IncMetricsRecordError(eventType, errorType string)
}

type HTTPMetricsRecorder interface {
	ObserveHTTPLatency(method, path, status string, elapsed time.Duration)
	IncHTTPRequests(method, path, status string)
	IncHTTPInFlight(method, path string)
	DecHTTPInFlight(method, path string)
}

const (
	RateLimitReasonExceeded = "limit_exceeded"
	RateLimitReasonError    = "limiter_error"
)

type RateLimitMetricsRecorder interface {
	IncRateLimited(limiter, clientIP, reason string)
}

const (
	WSEndpointConversations     = "conversations"
	WSEndpointCallSession       = "call_session"
	WSEndpointWorkflowSimulator = "workflow_simulator"
	WSEndpointWorkflowAIBuilder = "workflow_ai_builder"
)

type WSMetricsRecorder interface {
	IncWSConnections(endpoint string)
	DecWSConnections(endpoint string)
}
