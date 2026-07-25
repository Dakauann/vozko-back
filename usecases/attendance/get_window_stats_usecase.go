package attendance_usecase

import "vozko/domain/attendance"

type getWindowStatsUseCase struct {
	repo attendance.Repository
}

func NewGetWindowStatsUseCase(repo attendance.Repository) attendance.GetWindowStatsUseCase {
	return &getWindowStatsUseCase{repo: repo}
}

func (uc *getWindowStatsUseCase) Execute(workspaceID string, filter attendance.StatsFilter) (*attendance.WindowStats, error) {
	return uc.repo.GetWindowStats(workspaceID, filter)
}
