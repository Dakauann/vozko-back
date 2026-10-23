package emailsending_repository

import (
	"context"
	"testing"

	es "vozko/domain/emailsending"
)

func TestIntegrationTheWorkspaceListingCarriesEachDomainLifecycle(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	domains := NewDomainRepository(db)
	acct := newAccount(t, accounts, "loja")
	domain := func(name string, kind es.DomainKind) *es.SendingDomain {
		d := &es.SendingDomain{WorkspaceID: acct.WorkspaceID, AccountID: acct.ID, Domain: name, Kind: kind, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic, MailFromDomain: "bounce." + name, ReplyDomain: name}
		if err := domains.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	active := domain("ativa.envios.test", es.DomainSharedProbation)
	releasing := domain("removendo.envios.test", es.DomainSharedProbation)
	released := domain("removida.envios.test", es.DomainSharedProbation)
	removed := domain("loja.com.br", es.DomainOwn)
	releasing.Release()
	released.Release()
	for _, d := range []*es.SendingDomain{releasing, released} {
		if err := domains.SetRelease(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	released.Released()
	if err := domains.SetRelease(ctx, released); err != nil {
		t.Fatal(err)
	}
	removed.Retired = true
	if err := domains.Update(ctx, removed); err != nil {
		t.Fatal(err)
	}
	listing, err := domains.ListByWorkspace(ctx, acct.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]es.DomainLifecycle{
		active.ID:    es.LifecycleActive,
		releasing.ID: es.LifecycleReleasing,
		released.ID:  es.LifecycleRetired,
		removed.ID:   es.LifecycleRetired,
	}
	if len(listing) != len(want) {
		t.Fatalf("listing = %+v", listing)
	}
	listed := 0
	for _, d := range listing {
		if got := d.Lifecycle(); got != want[d.ID] {
			t.Fatalf("%s lifecycle = %q, want %q", d.Domain, got, want[d.ID])
		}
		if d.Listed() {
			listed++
		}
	}
	if listed != 2 {
		t.Fatalf("listed = %d, want the active and the releasing domain", listed)
	}
}
