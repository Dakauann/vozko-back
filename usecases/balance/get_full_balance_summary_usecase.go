package balance_usecase

import "vozko/domain/balance"

type getFullBalanceSummaryUseCase struct {
	repo balance.Repository
}

func NewGetFullBalanceSummaryUseCase(repo balance.Repository) balance.GetFullBalanceSummaryUseCase {
	return &getFullBalanceSummaryUseCase{repo: repo}
}

func (uc *getFullBalanceSummaryUseCase) Execute(workspaceID string) (*balance.FullBalanceSummary, error) {
	return uc.repo.GetFullBalanceSummary(workspaceID)
}
