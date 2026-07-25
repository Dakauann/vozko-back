package attendance

type GetAttendanceStatsUseCase interface {
	Execute(workspaceID string, filter StatsFilter) ([]AttendantStats, error)
}

type GetWindowStatsUseCase interface {
	Execute(workspaceID string, filter StatsFilter) (*WindowStats, error)
}

type GetResponseTimeDistributionUseCase interface {
	Execute(workspaceID string, filter StatsFilter) (*ResponseTimeDistribution, error)
}

type GetAIAgentStatsUseCase interface {
	Execute(workspaceID string, filter StatsFilter) ([]AIAgentStats, error)
}

type GetFRTStatsUseCase interface {
	Execute(workspaceID string, filter StatsFilter) (*FRTStats, error)
}
