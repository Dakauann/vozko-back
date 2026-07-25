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

// Reasons a request can be rejected by a per-IP rate limiter, used as the
// `reason` label on the rate-limit metric.
const (
	RateLimitReasonExceeded = "limit_exceeded" // the IP's window budget was spent
	RateLimitReasonError    = "limiter_error"  // the limiter backend failed (fail-closed reject)
)

// RateLimitMetricsRecorder records requests rejected by a per-IP rate limiter.
// The client_ip label is intentionally populated only on the rejection path
// (not per request), so cardinality is bounded to the small set of IPs that
// actually exceed a limit — typically shared-NAT offices whose many agents
// collide on one per-IP budget. That is precisely the signal we want to surface.
type RateLimitMetricsRecorder interface {
	IncRateLimited(limiter, clientIP, reason string)
}

const (
	WSEndpointConversations     = "conversations"
	WSEndpointDialer            = "dialer"
	WSEndpointWorkflowSimulator = "workflow_simulator"
	WSEndpointWorkflowAIBuilder = "workflow_ai_builder"
)

type WSMetricsRecorder interface {
	IncWSConnections(endpoint string)
	DecWSConnections(endpoint string)
}
