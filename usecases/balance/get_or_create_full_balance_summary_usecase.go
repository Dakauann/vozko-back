package balance_usecase

import "vozko/domain/balance"

const defaultCurrency = "USD"

type getOrCreateFullBalanceSummaryUseCase struct {
	repo balance.Repository
}

func NewGetOrCreateFullBalanceSummaryUseCase(repo balance.Repository) balance.GetOrCreateFullBalanceSummaryUseCase {
	return &getOrCreateFullBalanceSummaryUseCase{repo: repo}
}

func (uc *getOrCreateFullBalanceSummaryUseCase) Execute(workspaceID string) (*balance.FullBalanceSummary, error) {
	_, err := uc.repo.EnsureBalanceExists(workspaceID, defaultCurrency)
	if err != nil {
		return nil, err
	}

	return uc.repo.GetFullBalanceSummary(workspaceID)
}
