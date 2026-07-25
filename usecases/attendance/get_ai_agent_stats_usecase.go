package attendance_usecase

import "vozko/domain/attendance"

type getAIAgentStatsUseCase struct {
	repo attendance.Repository
}

func NewGetAIAgentStatsUseCase(repo attendance.Repository) attendance.GetAIAgentStatsUseCase {
	return &getAIAgentStatsUseCase{repo: repo}
}

func (uc *getAIAgentStatsUseCase) Execute(workspaceID string, filter attendance.StatsFilter) ([]attendance.AIAgentStats, error) {
	return uc.repo.GetAIAgentStats(workspaceID, filter)
}
