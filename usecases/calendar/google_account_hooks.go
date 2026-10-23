package calendar_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/calendar"
	"vozko/domain/googleaccount"
)

type WatchHooks struct {
	start calendar.StartWatchUseCase
	stop  calendar.StopWatchUseCase
}

func NewWatchHooks(start calendar.StartWatchUseCase, stop calendar.StopWatchUseCase) *WatchHooks {
	if start == nil || stop == nil {
		panic("calendar: watch hooks require start and stop")
	}
	return &WatchHooks{start: start, stop: stop}
}

func (h *WatchHooks) AfterConnect(_ context.Context, workspaceID string) error {
	_, err := h.start.Execute(workspaceID)
	return err
}

func (h *WatchHooks) BeforeDisconnect(_ context.Context, workspaceID string) error {
	return h.stop.Execute(workspaceID)
}

type ImportLegacyConnections struct {
	legacy   calendar.LegacyConnectionStore
	accounts googleaccount.Repository
	calendar calendar.Repository
}

func NewImportLegacyConnections(legacy calendar.LegacyConnectionStore, accounts googleaccount.Repository, repo calendar.Repository) *ImportLegacyConnections {
	if legacy == nil || accounts == nil || repo == nil {
		panic("calendar: the legacy import requires the legacy store, google accounts and the calendar repository")
	}
	return &ImportLegacyConnections{legacy: legacy, accounts: accounts, calendar: repo}
}

func (uc *ImportLegacyConnections) Execute(ctx context.Context) (int, error) {
	rows, err := uc.legacy.ListLegacyConnections(ctx)
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, row := range rows {
		done, err := uc.importOne(ctx, row)
		if err != nil {
			return imported, fmt.Errorf("calendar: import legacy connection %s: %w", row.ID, err)
		}
		if done {
			imported++
		}
		if err := uc.legacy.RetireLegacyConnection(ctx, row.ID); err != nil {
			return imported, err
		}
	}
	return imported, nil
}

func (uc *ImportLegacyConnections) importOne(ctx context.Context, row calendar.LegacyGoogleConnection) (bool, error) {
	_, err := uc.accounts.FindBound(ctx, row.WorkspaceID, googleaccount.ProductCalendar)
	switch {
	case err == nil:
		return false, nil
	case !errors.Is(err, googleaccount.ErrNotConnected):
		return false, err
	}
	if strings.TrimSpace(row.RefreshToken) == "" || googleaccount.NormalizeEmail(row.Email) == "" {
		log.Printf("[calendar] legacy connection %s of workspace %s has no refresh token or email; the workspace must connect Google again", row.ID, row.WorkspaceID)
		return false, nil
	}
	account := &googleaccount.Account{
		WorkspaceID:     row.WorkspaceID,
		Email:           row.Email,
		AccessToken:     row.AccessToken,
		RefreshToken:    row.RefreshToken,
		AccessExpiresAt: row.TokenExpiry,
		Scopes:          googleaccount.ProductCalendar.Scopes(),
	}
	if err := uc.accounts.SaveConnected(ctx, account); err != nil {
		return false, err
	}
	if err := uc.accounts.Bind(ctx, googleaccount.Binding{WorkspaceID: row.WorkspaceID, Product: googleaccount.ProductCalendar, AccountID: account.ID}); err != nil {
		return false, err
	}
	if strings.TrimSpace(row.SyncToken) != "" {
		if err := uc.calendar.SaveSyncToken(row.WorkspaceID, row.SyncToken); err != nil {
			return false, err
		}
	}
	return true, nil
}
