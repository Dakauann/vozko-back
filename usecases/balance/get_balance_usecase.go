package balance_usecase

import "vozko/domain/balance"

type getBalanceUseCase struct {
	repo balance.Repository
}

func NewGetBalanceUseCase(repo balance.Repository) balance.GetBalanceUseCase {
	return &getBalanceUseCase{repo: repo}
}

func (uc *getBalanceUseCase) Execute(workspaceID string) (*balance.Balance, error) {
	return uc.repo.GetByWorkspaceID(workspaceID)
}
