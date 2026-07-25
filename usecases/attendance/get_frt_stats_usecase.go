package attendance_usecase

import "vozko/domain/attendance"

type getFRTStatsUseCase struct {
	repo attendance.Repository
}

func NewGetFRTStatsUseCase(repo attendance.Repository) attendance.GetFRTStatsUseCase {
	return &getFRTStatsUseCase{repo: repo}
}

func (uc *getFRTStatsUseCase) Execute(workspaceID string, filter attendance.StatsFilter) (*attendance.FRTStats, error) {
	return uc.repo.GetFRTStats(workspaceID, filter)
}
