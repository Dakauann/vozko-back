package shipping_usecase

import (
	"context"

	"vozko/domain/shipping"
)

type ListProviderAccounts struct {
	repo shipping.ProviderAccountRepository
}

func NewListProviderAccounts(repo shipping.ProviderAccountRepository) shipping.ListProviderAccountsUseCase {
	return &ListProviderAccounts{repo: repo}
}

func (uc *ListProviderAccounts) Execute(ctx context.Context, input shipping.ListProviderAccountsInput) ([]*shipping.ProviderAccount, error) {
	if input.Provider == "" {
		return nil, shipping.ErrProviderNotConfigured
	}
	return uc.repo.ListByWorkspace(input.UserID, input.Provider)
}
