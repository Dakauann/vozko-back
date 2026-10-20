package mercadolivre

import (
	"context"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

var _ conversation.WindowStatesAdapter = (*ChannelAdapter)(nil)

func (a *ChannelAdapter) WindowStates(ctx context.Context, entryIDs []string) (map[string]conversation.WindowState, error) {
	convs, err := a.conversations.FindByIDs(ctx, shared.UniqueNonEmpty(entryIDs))
	if err != nil {
		return nil, err
	}
	accountIDs := make([]string, 0, len(convs))
	for _, conv := range convs {
		accountIDs = append(accountIDs, conv.AccountID)
	}
	accounts, err := a.accounts.FindByIDs(ctx, shared.UniqueNonEmpty(accountIDs))
	if err != nil {
		return nil, err
	}
	accountByID := make(map[string]*mldomain.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	out := make(map[string]conversation.WindowState, len(convs))
	for _, conv := range convs {
		out[conv.ID] = a.windowOf(accountByID[conv.AccountID], conv)
	}
	return out, nil
}
