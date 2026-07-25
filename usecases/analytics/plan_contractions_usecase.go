package analytics_usecase

import analytics_domain "vozko/domain/analytics"

type getPlanContractionsUseCase struct {
	repo analytics_domain.Repository
}

func NewGetPlanContractionsUseCase(repo analytics_domain.Repository) analytics_domain.GetPlanContractionsUseCase {
	return &getPlanContractionsUseCase{repo: repo}
}

func (uc *getPlanContractionsUseCase) Execute(input analytics_domain.PlanContractionsInput) (*analytics_domain.PlanContractionsReport, error) {
	return uc.repo.GetPlanContractions(input)
}
