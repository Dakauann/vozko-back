package balance_usecase

import "vozko/domain/balance"

type checkBalanceUseCase struct {
	repo balance.Repository
}

func NewCheckBalanceUseCase(repo balance.Repository) balance.CheckBalanceUseCase {
	return &checkBalanceUseCase{repo: repo}
}

func (uc *checkBalanceUseCase) Execute(workspaceID string, amount int64) (bool, error) {
	return uc.repo.HasSufficientBalance(workspaceID, amount)
}
