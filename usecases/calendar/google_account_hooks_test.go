package calendar_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/googleaccount"
)

type legacyStoreFake struct {
	rows    []calendar.LegacyGoogleConnection
	retired []string
}

func (s *legacyStoreFake) ListLegacyConnections(context.Context) ([]calendar.LegacyGoogleConnection, error) {
	return s.rows, nil
}

func (s *legacyStoreFake) RetireLegacyConnection(_ context.Context, id string) error {
	s.retired = append(s.retired, id)
	return nil
}

type accountsFake struct {
	googleaccount.Repository
	bound map[string]string
	saved []*googleaccount.Account
}

func (a *accountsFake) FindBound(_ context.Context, ws string, p googleaccount.Product) (*googleaccount.Account, error) {
	if id, ok := a.bound[ws+"|"+string(p)]; ok {
		return &googleaccount.Account{ID: id}, nil
	}
	return nil, googleaccount.ErrNotConnected
}

func (a *accountsFake) SaveConnected(_ context.Context, acc *googleaccount.Account) error {
	acc.ID = "acct-" + acc.WorkspaceID
	a.saved = append(a.saved, acc)
	return nil
}

func (a *accountsFake) Bind(_ context.Context, b googleaccount.Binding) error {
	a.bound[b.WorkspaceID+"|"+string(b.Product)] = b.AccountID
	return nil
}

func TestImportLegacyConnectionsMovesEachWorkspaceOnceAndRetiresEveryRow(t *testing.T) {
	store := &legacyStoreFake{rows: []calendar.LegacyGoogleConnection{
		{ID: "l1", WorkspaceID: "ws-new", Email: "Ops@Example.com", AccessToken: "a", RefreshToken: "r", TokenExpiry: time.Now(), SyncToken: "sync-1"},
		{ID: "l2", WorkspaceID: "ws-already", Email: "x@example.com", AccessToken: "a", RefreshToken: "r"},
		{ID: "l3", WorkspaceID: "ws-broken", Email: "y@example.com", AccessToken: "a"},
	}}
	accounts := &accountsFake{bound: map[string]string{"ws-already|calendar": "existing"}}
	repo := &watchRepoMock{}
	imported, err := NewImportLegacyConnections(store, accounts, repo).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if imported != 1 || len(accounts.saved) != 1 {
		t.Fatalf("imported %d, saved %d; want 1", imported, len(accounts.saved))
	}
	saved := accounts.saved[0]
	if !googleaccount.GrantCovers(saved.Scopes, googleaccount.ProductCalendar) || saved.RefreshToken != "r" {
		t.Fatalf("saved = %+v", saved)
	}
	if accounts.bound["ws-new|calendar"] != saved.ID || repo.updatedSyncToken != "sync-1" {
		t.Fatalf("binding %q, sync token %q", accounts.bound["ws-new|calendar"], repo.updatedSyncToken)
	}
	if len(store.retired) != 3 {
		t.Fatalf("retired %v; every legacy row must be retired so a later disconnect is not undone on the next boot", store.retired)
	}
}

type sourceFake struct{ err error }

func (s sourceFake) Access(context.Context, string, googleaccount.Product) (googleaccount.Access, error) {
	return googleaccount.Access{Token: "t"}, s.err
}

func TestAccessProviderMapsLostAccountsToNotConnected(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		notConnected bool
	}{
		{name: "never connected", err: googleaccount.ErrNotConnected, notConnected: true},
		{name: "needs reconnect", err: googleaccount.ErrNeedsReconnect, notConnected: true},
		{name: "google unavailable", err: errors.New("503")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewAccessProvider(sourceFake{err: c.err}).CalendarAccess(context.Background(), "ws")
			if got := errors.Is(err, calendar.ErrGoogleNotConnected); got != c.notConnected {
				t.Fatalf("ErrGoogleNotConnected = %v, want %v (err %v)", got, c.notConnected, err)
			}
		})
	}
}
