package balance_usecase

import (
	"vozko/domain/balance"
)

type getOrCreateBalanceUseCase struct {
	repo balance.Repository
}

func NewGetOrCreateBalanceUseCase(repo balance.Repository) balance.GetOrCreateBalanceUseCase {
	return &getOrCreateBalanceUseCase{repo: repo}
}

func (uc *getOrCreateBalanceUseCase) Execute(workspaceID string) (*balance.Balance, error) {
	return uc.repo.EnsureBalanceExists(workspaceID, "USD")
}
