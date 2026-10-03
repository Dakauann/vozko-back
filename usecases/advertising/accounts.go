package advertising

import (
	"context"

	ads "vozko/domain/advertising"
)

type AccountsUseCase struct {
	accounts ads.AccountRepository
}

func NewAccountsUseCase(accounts ads.AccountRepository) *AccountsUseCase {
	return &AccountsUseCase{accounts: accounts}
}

func (uc *AccountsUseCase) List(ctx context.Context, workspaceID string) ([]*ads.AdAccount, error) {
	return uc.accounts.ListByWorkspace(ctx, workspaceID)
}

func (uc *AccountsUseCase) Disconnect(ctx context.Context, workspaceID, accountID string) error {
	account, err := uc.accounts.FindByID(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	return uc.accounts.SetConnection(ctx, account.ID, ads.ConnectionDisconnected)
}
