package emailsending_repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	es "vozko/domain/emailsending"
)

func newAccount(t *testing.T, accounts es.AccountRepository, slug string) *es.SenderAccount {
	t.Helper()
	ws := uuid.NewString()
	acct := &es.SenderAccount{WorkspaceID: ws, Provider: "ses", TenantName: "ws-" + ws, Mode: es.ModeProbation, Status: es.AccountActive, Slug: slug, ProbationStartedAt: time.Now().UTC()}
	if err := accounts.Create(context.Background(), acct); err != nil {
		t.Fatal(err)
	}
	return acct
}

func TestIntegrationTwoWorkspacesRacingForOneSlugGetItOnce(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	newAccount(t, accounts, "joana")
	if err := accounts.Create(ctx, &es.SenderAccount{WorkspaceID: uuid.NewString(), Provider: "ses", TenantName: "ws-" + uuid.NewString(), Mode: es.ModeProbation, Status: es.AccountActive, Slug: "joana"}); !errors.Is(err, es.ErrSlugTaken) {
		t.Fatalf("a second account on a taken slug = %v, want ErrSlugTaken", err)
	}

	racers := []*es.SenderAccount{newAccount(t, accounts, "ana"), newAccount(t, accounts, "bruno"), newAccount(t, accounts, "carla")}
	var wg sync.WaitGroup
	results := make([]error, len(racers))
	for i, acct := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = accounts.ChangeSlug(ctx, acct.ID, "cartaodetodososasco")
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range results {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, es.ErrSlugTaken):
			t.Fatalf("unexpected error %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d workspaces got the slug, want exactly one", won)
	}
	if err := accounts.ChangeSlug(ctx, uuid.NewString(), "ninguem"); !errors.Is(err, es.ErrAccountNotFound) {
		t.Fatalf("unknown account = %v", err)
	}
}

func TestIntegrationSentFromSharedDomainsCountsOnlyTheSharedOnes(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	domains := NewDomainRepository(db)
	messages := NewMessageRepository(db)
	activity := NewSendActivity(db)
	acct := newAccount(t, accounts, "loja")
	shared := &es.SendingDomain{WorkspaceID: acct.WorkspaceID, AccountID: acct.ID, Domain: "loja.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic, MailFromDomain: "bounce.loja.envios.test", ReplyDomain: "loja.envios.test"}
	own := &es.SendingDomain{WorkspaceID: acct.WorkspaceID, AccountID: acct.ID, Domain: "loja.com.br", Kind: es.DomainOwn, Status: es.DomainVerified, ConnectMethod: es.ConnectManual, MailFromDomain: "bounce.loja.com.br", ReplyDomain: "reply.loja.com.br"}
	for _, d := range []*es.SendingDomain{shared, own} {
		if err := domains.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if sent, err := activity.SentFromSharedDomains(ctx, acct.WorkspaceID); err != nil || sent {
		t.Fatalf("before any send = %v %v", sent, err)
	}
	seedMessage(t, messages, acct.WorkspaceID, own.ID, "ses-"+uuid.NewString(), time.Now().UTC())
	if sent, err := activity.SentFromSharedDomains(ctx, acct.WorkspaceID); err != nil || sent {
		t.Fatalf("after an own domain send = %v %v", sent, err)
	}
	seedMessage(t, messages, acct.WorkspaceID, shared.ID, "ses-"+uuid.NewString(), time.Now().UTC())
	if sent, err := activity.SentFromSharedDomains(ctx, acct.WorkspaceID); err != nil || !sent {
		t.Fatalf("after a shared domain send = %v %v", sent, err)
	}
	if sent, err := activity.SentFromSharedDomains(ctx, uuid.NewString()); err != nil || sent {
		t.Fatalf("another workspace = %v %v", sent, err)
	}
}

func TestIntegrationAReleasedDomainIsListedUntilTheProviderIsCleaned(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	accounts := NewAccountRepository(db)
	domains := NewDomainRepository(db)
	acct := newAccount(t, accounts, "loja")
	d := &es.SendingDomain{WorkspaceID: acct.WorkspaceID, AccountID: acct.ID, Domain: "loja.envios.test", Kind: es.DomainSharedProbation, Status: es.DomainVerified, ConnectMethod: es.ConnectAutomatic, MailFromDomain: "bounce.loja.envios.test", ReplyDomain: "loja.envios.test"}
	if err := domains.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	stale := *d
	d.Release()
	if err := domains.SetRelease(ctx, d); err != nil {
		t.Fatal(err)
	}
	stale.Status = es.DomainVerified
	if err := domains.Update(ctx, &stale); err != nil {
		t.Fatal(err)
	}
	listed, err := domains.ListPending(ctx, 10)
	if err != nil || len(listed) != 1 || !listed[0].AwaitsRelease() || !listed[0].Retired {
		t.Fatalf("a stale write must not undo the release: %+v %v", listed, err)
	}
	d.Released()
	if err := domains.SetRelease(ctx, d); err != nil {
		t.Fatal(err)
	}
	if listed, err = domains.ListPending(ctx, 10); err != nil || len(listed) != 0 {
		t.Fatalf("after the cleanup = %+v %v", listed, err)
	}
	stored, err := domains.Get(ctx, acct.WorkspaceID, d.ID)
	if err != nil || stored.ReleaseState != es.ReleaseDone || !stored.Retired {
		t.Fatalf("stored = %+v %v", stored, err)
	}
	stored.Reinstate()
	if err := domains.SetRelease(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err := domains.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if back, _ := domains.Get(ctx, acct.WorkspaceID, d.ID); back.Retired || back.ReleaseState != es.ReleaseNone || back.Status != es.DomainPending {
		t.Fatalf("reinstated = %+v", back)
	}
}
