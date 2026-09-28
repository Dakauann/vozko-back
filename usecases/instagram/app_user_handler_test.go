package instagram

import (
	"context"
	"errors"
	"testing"

	igdomain "vozko/domain/instagram"
)

func revokableAccount() *igdomain.Account {
	return &igdomain.Account{ID: "acc-1", IGUserID: "ig-1", Status: igdomain.StatusConnected}
}

func TestRevokeAppUserMarksTheAccountRevoked(t *testing.T) {
	repo := &fakeAccountRepo{FindByIGUserIDFn: func(context.Context, string) (*igdomain.Account, error) {
		return revokableAccount(), nil
	}}
	if err := NewAppUserHandler(repo).RevokeAppUser(context.Background(), "ig-1"); err != nil {
		t.Fatal(err)
	}
	if len(repo.StatusUpdates) != 1 || repo.StatusUpdates[0] != igdomain.StatusRevoked {
		t.Fatalf("status updates = %v", repo.StatusUpdates)
	}
}

func TestRevokeUnknownAppUserIsANoOp(t *testing.T) {
	repo := &fakeAccountRepo{}
	if err := NewAppUserHandler(repo).RevokeAppUser(context.Background(), "ig-404"); err != nil {
		t.Fatalf("unknown user must not fail the callback: %v", err)
	}
	if len(repo.StatusUpdates) != 0 {
		t.Fatal("no account should change")
	}
}

func TestRevokeSurfacesRepositoryFailures(t *testing.T) {
	repo := &fakeAccountRepo{FindByIGUserIDFn: func(context.Context, string) (*igdomain.Account, error) {
		return nil, errors.New("db down")
	}}
	if err := NewAppUserHandler(repo).RevokeAppUser(context.Background(), "ig-1"); err == nil {
		t.Fatal("repository failure swallowed")
	}
}

func TestEraseAppUserRevokesWipesTheTokenAndDeletes(t *testing.T) {
	repo := &fakeAccountRepo{FindByIGUserIDFn: func(context.Context, string) (*igdomain.Account, error) {
		return revokableAccount(), nil
	}}
	if err := NewAppUserHandler(repo).EraseAppUser(context.Background(), "ig-1"); err != nil {
		t.Fatal(err)
	}
	if repo.ErasedTokens != 1 || repo.Deletes != 1 || len(repo.StatusUpdates) != 1 {
		t.Fatalf("erased=%d deletes=%d statuses=%v", repo.ErasedTokens, repo.Deletes, repo.StatusUpdates)
	}
}
