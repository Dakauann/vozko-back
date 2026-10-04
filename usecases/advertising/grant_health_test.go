package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

type fakeTokenInspector struct {
	debug *fbdomain.TokenDebug
	err   error
	seen  []string
}

func (f *fakeTokenInspector) DebugToken(_ context.Context, token string) (*fbdomain.TokenDebug, error) {
	f.seen = append(f.seen, token)
	return f.debug, f.err
}

func healthWorld(debug *fbdomain.TokenDebug, err error) (*world, *GrantHealthUseCase, *fakeTokenInspector) {
	w := newWorld()
	w.grants.byID["grant-1"].AppScopedUserID = "asid-1"
	inspector := &fakeTokenInspector{debug: debug, err: err}
	uc := NewGrantHealthUseCase(w.grants, w.accounts, inspector)
	uc.now = func() time.Time { return testNow }
	return w, uc, inspector
}

func TestAHealthyGrantKeepsItsAccountsAndRecordsTheScopesMetaStillGrants(t *testing.T) {
	w, uc, inspector := healthWorld(&fbdomain.TokenDebug{Valid: true, Scopes: []string{"ads_management"}}, nil)
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(inspector.seen) != 1 || inspector.seen[0] != "tok" {
		t.Fatalf("checked %v", inspector.seen)
	}
	if w.grants.checked["grant-1"] == nil || w.grants.revoked["grant-1"] || w.accounts.byID["acc-1"].Connection != ads.ConnectionConnected {
		t.Fatalf("checked %v revoked %v account %s", w.grants.checked, w.grants.revoked, w.accounts.byID["acc-1"].Connection)
	}
}

func TestAnInvalidGrantIsRevokedAndItsAccountsAskToReconnect(t *testing.T) {
	w, uc, _ := healthWorld(&fbdomain.TokenDebug{Valid: false}, nil)
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !w.grants.revoked["grant-1"] || w.accounts.byID["acc-1"].Connection != ads.ConnectionNeedsReconnect {
		t.Fatalf("revoked %v account %s", w.grants.revoked, w.accounts.byID["acc-1"].Connection)
	}
}

func TestAnExpiredGrantIsRevoked(t *testing.T) {
	expired := testNow.Add(-time.Hour)
	w, uc, _ := healthWorld(&fbdomain.TokenDebug{Valid: true, ExpiresAt: &expired}, nil)
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !w.grants.revoked["grant-1"] {
		t.Fatal("an expired token stayed active")
	}
}

func TestMetaRejectingTheTokenRevokesTheGrant(t *testing.T) {
	w, uc, _ := healthWorld(nil, &meta.Error{Code: 190, Message: "Error validating access token"})
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !w.grants.revoked["grant-1"] {
		t.Fatal("a token Meta rejected stayed active")
	}
}

func TestAnOutageNeverRevokesAGrant(t *testing.T) {
	w, uc, _ := healthWorld(nil, errors.New("connection reset"))
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.grants.revoked["grant-1"] || w.accounts.byID["acc-1"].Connection != ads.ConnectionConnected {
		t.Fatal("a network error revoked a working grant")
	}
}

func TestMetaRemovingTheUserRevokesTheirGrants(t *testing.T) {
	w, uc, _ := healthWorld(nil, nil)
	if err := uc.RevokeAppUser(context.Background(), "asid-1"); err != nil {
		t.Fatal(err)
	}
	if !w.grants.revoked["grant-1"] || w.accounts.byID["acc-1"].Connection != ads.ConnectionNeedsReconnect || w.grants.erased["grant-1"] {
		t.Fatalf("revoked %v erased %v account %s", w.grants.revoked, w.grants.erased, w.accounts.byID["acc-1"].Connection)
	}
	if err := uc.RevokeAppUser(context.Background(), "asid-other"); err != nil {
		t.Fatal(err)
	}
}

func TestADataDeletionRequestAlsoErasesTheToken(t *testing.T) {
	w, uc, _ := healthWorld(nil, nil)
	if err := uc.EraseAppUser(context.Background(), "asid-1"); err != nil {
		t.Fatal(err)
	}
	if !w.grants.revoked["grant-1"] || !w.grants.erased["grant-1"] {
		t.Fatalf("revoked %v erased %v", w.grants.revoked, w.grants.erased)
	}
}
