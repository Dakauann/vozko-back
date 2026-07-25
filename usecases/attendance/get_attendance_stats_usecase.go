package attendance_usecase

import "vozko/domain/attendance"

type getAttendanceStatsUseCase struct {
	repo attendance.Repository
}

func NewGetAttendanceStatsUseCase(repo attendance.Repository) attendance.GetAttendanceStatsUseCase {
	return &getAttendanceStatsUseCase{repo: repo}
}

func (uc *getAttendanceStatsUseCase) Execute(workspaceID string, filter attendance.StatsFilter) ([]attendance.AttendantStats, error) {
	return uc.repo.GetAttendantStats(workspaceID, filter)
}
