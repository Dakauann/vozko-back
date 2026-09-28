package instagram

import (
	"context"
	"errors"
	"time"

	igdomain "vozko/domain/instagram"
	"vozko/domain/metaplatform"
)

type AppUserHandler struct {
	accounts igdomain.AccountRepository
}

func NewAppUserHandler(accounts igdomain.AccountRepository) *AppUserHandler {
	return &AppUserHandler{accounts: accounts}
}

var _ metaplatform.AppUserHandler = (*AppUserHandler)(nil)

func (h *AppUserHandler) RevokeAppUser(ctx context.Context, appScopedUserID string) error {
	account, err := h.find(ctx, appScopedUserID)
	if err != nil || account == nil {
		return err
	}
	return h.revoke(ctx, account)
}

func (h *AppUserHandler) EraseAppUser(ctx context.Context, appScopedUserID string) error {
	account, err := h.find(ctx, appScopedUserID)
	if err != nil || account == nil {
		return err
	}
	if err := h.revoke(ctx, account); err != nil {
		return err
	}
	if err := h.accounts.UpdateToken(ctx, account.ID, "", time.Time{}, time.Time{}); err != nil {
		return err
	}
	return h.accounts.Delete(ctx, account.ID)
}

func (h *AppUserHandler) find(ctx context.Context, igUserID string) (*igdomain.Account, error) {
	account, err := h.accounts.FindByIGUserID(ctx, igUserID)
	if errors.Is(err, igdomain.ErrAccountNotFound) {
		return nil, nil
	}
	return account, err
}

func (h *AppUserHandler) revoke(ctx context.Context, account *igdomain.Account) error {
	if !account.Status.CanTransitionTo(igdomain.StatusRevoked) {
		return nil
	}
	return h.accounts.UpdateStatus(ctx, account.ID, igdomain.StatusRevoked, "access removed in Instagram")
}
