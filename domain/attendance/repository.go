package attendance

type Repository interface {
	GetAttendantStats(workspaceID string, filter StatsFilter) ([]AttendantStats, error)

	GetWindowStats(workspaceID string, filter StatsFilter) (*WindowStats, error)

	GetResponseTimeDistribution(workspaceID string, filter StatsFilter) (*ResponseTimeDistribution, error)

	GetAIAgentStats(workspaceID string, filter StatsFilter) ([]AIAgentStats, error)

	GetFRTStats(workspaceID string, filter StatsFilter) (*FRTStats, error)

	GetOverview(workspaceID string, filter OverviewFilter) (*Overview, error)
}
