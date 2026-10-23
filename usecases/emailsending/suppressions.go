package emailsending

import (
	"context"
	"strings"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

type Suppressions struct {
	c *core
}

func (s *Suppressions) List(ctx context.Context, workspaceID, search string, offset, limit int) ([]es.Suppression, int64, error) {
	q := es.SuppressionQuery{Prefix: strings.ToLower(strings.TrimSpace(search)), Offset: offset, Limit: limit}
	return s.c.Suppressions.List(ctx, workspaceID, q)
}

func (s *Suppressions) Add(ctx context.Context, workspaceID, rawAddress string) (es.Suppression, error) {
	address, err := emailaddress.Normalize(rawAddress)
	if err != nil {
		return es.Suppression{}, ErrInvalidAddress
	}
	acct, err := s.c.account(ctx, workspaceID)
	if err != nil {
		return es.Suppression{}, err
	}
	entry := es.Suppression{WorkspaceID: workspaceID, Address: address, Reason: es.SuppressManual, SuppressedAt: s.c.Now()}
	if err := s.c.Suppressions.Suppress(ctx, entry); err != nil {
		return es.Suppression{}, err
	}
	if acct.Status == es.AccountDormant {
		return entry, nil
	}
	if err := s.c.Provider.SuppressAddress(ctx, acct.TenantName, address, es.SuppressManual); err != nil {
		return entry, providerFailure("suppress", err)
	}
	return entry, nil
}

func (s *Suppressions) Remove(ctx context.Context, workspaceID, rawAddress string) error {
	address, err := emailaddress.Normalize(rawAddress)
	if err != nil {
		return ErrInvalidAddress
	}
	acct, err := s.c.account(ctx, workspaceID)
	if err != nil {
		return err
	}
	found, err := s.c.Suppressions.Suppressed(ctx, workspaceID, []emailaddress.Address{address})
	if err != nil {
		return err
	}
	reason, ok := found[address]
	if !ok {
		return ErrSuppressionNotFound
	}
	if !reason.Stored() {
		return ErrSuppressionNotFound
	}
	if !reason.Removable() {
		return ErrSuppressionLocked
	}
	if acct.Status == es.AccountDormant {
		return s.c.Suppressions.Remove(ctx, workspaceID, address)
	}
	if err := s.c.Provider.UnsuppressAddress(ctx, acct.TenantName, address); err != nil {
		return providerFailure("unsuppress", err)
	}
	return s.c.Suppressions.Remove(ctx, workspaceID, address)
}
