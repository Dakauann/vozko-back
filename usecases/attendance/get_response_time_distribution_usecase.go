package attendance_usecase

import "vozko/domain/attendance"

type getResponseTimeDistributionUseCase struct {
	repo attendance.Repository
}

func NewGetResponseTimeDistributionUseCase(repo attendance.Repository) attendance.GetResponseTimeDistributionUseCase {
	return &getResponseTimeDistributionUseCase{repo: repo}
}

func (uc *getResponseTimeDistributionUseCase) Execute(workspaceID string, filter attendance.StatsFilter) (*attendance.ResponseTimeDistribution, error) {
	return uc.repo.GetResponseTimeDistribution(workspaceID, filter)
}
