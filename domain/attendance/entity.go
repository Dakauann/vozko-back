package attendance

import "time"

type AttendantStats struct {
	UserID              string  `json:"user_id"`
	Username            string  `json:"username"`
	Email               string  `json:"email"`
	Role                string  `json:"role"`
	AssignedCount       int64   `json:"assigned_count"`
	RespondedCount      int64   `json:"responded_count"`
	ResponseRate        float64 `json:"response_rate"`
	AvgResponseTimeMins float64 `json:"avg_response_time_mins"`
	// ActorKind is always "human" for workspace members; AI stats use AIAgentStats.
	ActorKind string `json:"actor_kind,omitempty"`
}

// AIAgentStats is attendance metrics for an AI agent treated as an attendant.
type AIAgentStats struct {
	AgentID         string  `json:"agent_id"`
	Sessions        int64   `json:"sessions"`
	Contained       int64   `json:"contained"`
	HandedOff       int64   `json:"handed_off"`
	Abandoned       int64   `json:"abandoned"`
	ContainmentRate float64 `json:"containment_rate"`
	HandoffRate     float64 `json:"handoff_rate"`
	AvgAIMessages   float64 `json:"avg_ai_messages"`
}

// FRTStats is first-response time computed from assignment_history ownership start
// to first agent-side message (operator or ai_response).
type FRTStats struct {
	AvgFRTMins    float64 `json:"avg_frt_mins"`
	MedianFRTMins float64 `json:"median_frt_mins"`
	SampleCount   int64   `json:"sample_count"`
	HumanAvgMins  float64 `json:"human_avg_mins"`
	AIAvgMins     float64 `json:"ai_avg_mins"`
	HumanSamples  int64   `json:"human_samples"`
	AISamples     int64   `json:"ai_samples"`
}

type WindowBucket struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

type WindowStats struct {
	TotalOpen int64          `json:"total_open"`
	Buckets   []WindowBucket `json:"buckets"`
}

type ResponseTimeBucket struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

type ResponseTimeDistribution struct {
	Buckets []ResponseTimeBucket `json:"buckets"`
	Total   int64                `json:"total"`
}

type StatsFilter struct {
	DateFrom     *time.Time
	DateTo       *time.Time
	CampaignID   string
	CampaignType string
}
