package tiktok

import (
	"context"
	"errors"
	"testing"

	ttdomain "vozko/domain/tiktok"
)

type failingAccounts struct {
	*fakeAccounts
	err error
}

func (f failingAccounts) FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*ttdomain.Account, error) {
	return nil, f.err
}

func TestAccountOwnershipAnswersOnlyForTheOwningWorkspace(t *testing.T) {
	accounts := newFakeAccounts()
	account := accounts.put(alertAccount(ttdomain.StatusConnected))
	ownership := NewAccountOwnership(accounts)
	ctx := context.Background()

	if err := ownership.VerifyAccount(ctx, account.WorkspaceID, account.ID); err != nil {
		t.Fatalf("owner refused: %v", err)
	}
	if ok, err := ownership.AccountBelongsTo(ctx, account.WorkspaceID, account.ID); !ok || err != nil {
		t.Fatalf("owner = %v, %v", ok, err)
	}
	if ok, err := ownership.AccountBelongsTo(ctx, "another-workspace", account.ID); ok || err != nil {
		t.Fatalf("another workspace = %v, %v", ok, err)
	}
	broken := errors.New("database down")
	if ok, err := NewAccountOwnership(failingAccounts{fakeAccounts: accounts, err: broken}).AccountBelongsTo(ctx, account.WorkspaceID, account.ID); ok || !errors.Is(err, broken) {
		t.Fatalf("a failed lookup must refuse and surface the error: %v, %v", ok, err)
	}
}
