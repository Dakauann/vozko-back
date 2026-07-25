package attendance

type Repository interface {
	GetAttendantStats(workspaceID string, filter StatsFilter) ([]AttendantStats, error)

	GetWindowStats(workspaceID string, filter StatsFilter) (*WindowStats, error)

	GetResponseTimeDistribution(workspaceID string, filter StatsFilter) (*ResponseTimeDistribution, error)

	// GetAIAgentStats aggregates ai_attendance_sessions by agent (optional; may be nil-safe).
	GetAIAgentStats(workspaceID string, filter StatsFilter) ([]AIAgentStats, error)

	// GetFRTStats computes assignment-aware first response times.
	GetFRTStats(workspaceID string, filter StatsFilter) (*FRTStats, error)

	// GetOverview returns the full filterable ops dashboard payload.
	GetOverview(workspaceID string, filter OverviewFilter) (*Overview, error)
}
