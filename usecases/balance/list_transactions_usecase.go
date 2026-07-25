package balance_usecase

import (
	"vozko/domain/balance"
	"vozko/domain/shared"
)

type listTransactionsUseCase struct {
	repo balance.Repository
}

func NewListTransactionsUseCase(repo balance.Repository) balance.ListTransactionsUseCase {
	return &listTransactionsUseCase{repo: repo}
}

func (uc *listTransactionsUseCase) Execute(input balance.ListTransactionsInput) (*shared.PaginatedResult[*balance.Transaction], error) {
	return uc.repo.ListTransactions(input)
}
