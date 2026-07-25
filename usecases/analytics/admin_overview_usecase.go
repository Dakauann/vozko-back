package analytics_usecase

import analytics_domain "vozko/domain/analytics"

type getAdminOverviewUseCase struct {
	repo analytics_domain.Repository
}

func NewGetAdminOverviewUseCase(repo analytics_domain.Repository) analytics_domain.GetAdminOverviewUseCase {
	return &getAdminOverviewUseCase{repo: repo}
}

func (uc *getAdminOverviewUseCase) Execute(input analytics_domain.AdminOverviewInput) (*analytics_domain.AdminOverview, error) {
	return uc.repo.GetAdminOverview(input)
}
