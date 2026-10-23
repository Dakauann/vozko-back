package emailwiring_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

const e2eDay = 24 * time.Hour

func (w *world) activatedAgo(ws *tenant, age time.Duration) {
	w.t.Helper()
	w.must(w.db.Exec("UPDATE email_sender_accounts SET created_at = ? WHERE workspace_id = ?", time.Now().UTC().Add(-age), ws.ID).Error)
}

func (w *world) recordsUnder(sharedDomain, fqdn string) int {
	w.t.Helper()
	n := 0
	for _, r := range w.zones.published(sharedDomain) {
		if r.Name == fqdn || strings.HasSuffix(r.Name, "."+fqdn) {
			n++
		}
	}
	return n
}

func TestEmailE2E_OnlyAWorkspaceWithAPaidPlanCanActivateEmail(t *testing.T) {
	w := newWorld(t)
	unpaid := w.newTenant("Loja Sem Plano", 0)
	w.plans.lapse(unpaid.ID)

	status, err := w.module.Setup.Status(w.ctx, unpaid.ID)
	w.must(err)
	if status.Provisioned || !errors.Is(status.Activation, es.ErrPlanRequired) {
		t.Fatalf("status = %+v, want the activation refused up front", status)
	}
	if _, err := w.module.Setup.Provision(w.ctx, unpaid.ID, ""); !errors.Is(err, es.ErrPlanRequired) {
		t.Fatalf("provision = %v, want the paid plan refusal", err)
	}
	if _, err := w.accounts.GetByWorkspace(w.ctx, unpaid.ID); !errors.Is(err, es.ErrAccountNotFound) {
		t.Fatalf("a refused activation stored an account: %v", err)
	}
	if kicks := w.queue.readyCount(es.SetupFireTopic); kicks != 0 || len(w.zones.fake.Requests()) != 0 {
		t.Fatalf("a refused activation queued %d runs and made %d DNS calls", kicks, len(w.zones.fake.Requests()))
	}

	paid := w.sendingTenant("Loja Com Plano", 0)
	w.plans.lapse(paid.ID)
	if _, err := w.module.Domains.Connect(w.ctx, paid.ID, "lojacomplano.com.br"); !errors.Is(err, es.ErrPlanRequired) {
		t.Fatalf("connect after the plan lapsed = %v", err)
	}
	if acct := w.account(paid); acct.Status != es.AccountActive || !w.ses.HasTenant(acct.TenantName) {
		t.Fatalf("a lapsed plan tore down an active account: %+v", acct)
	}
}

func TestEmailE2E_AnAccountThatNeverSentIsWarnedThenReleasedAndItsNameIsFree(t *testing.T) {
	w := newWorld(t)
	ws := w.newTenant("Loja Esquecida", 0)
	_, err := w.module.Setup.Provision(w.ctx, ws.ID, "lojaesquecida")
	w.must(err)
	w.runSetup()
	w.verifyDomains(ws)
	acct := w.account(ws)
	fqdn := "lojaesquecida." + probationDomain
	if w.recordsUnder(probationDomain, fqdn) == 0 || !w.ses.HasIdentity(fqdn) || len(w.ses.Receiving(fqdn)) == 0 {
		t.Fatal("the subdomain was not set up before the test")
	}

	w.activatedAgo(ws, 23*e2eDay)
	now := time.Now().UTC()
	w.must(w.module.UnusedRelease.Sweep(w.ctx, now))
	key := "email_unused_release:" + ws.ID
	if w.alerts.publishedTimes(key) != 1 || w.account(ws).UnusedWarnedAt == nil {
		t.Fatalf("warnings = %d, stamp %v", w.alerts.publishedTimes(key), w.account(ws).UnusedWarnedAt)
	}
	w.must(w.module.UnusedRelease.Sweep(w.ctx, now.Add(3*e2eDay)))
	if w.alerts.publishedTimes(key) != 2 || !w.ses.HasTenant(acct.TenantName) {
		t.Fatal("before the deadline the sweep only warns again")
	}

	w.must(w.module.UnusedRelease.Sweep(w.ctx, now.Add(8*e2eDay)))
	if w.ses.HasIdentity(fqdn) || len(w.ses.Receiving(fqdn)) != 0 || w.ses.HasTenant(acct.TenantName) {
		t.Fatalf("identity %v receiving %v tenant %v after the release", w.ses.HasIdentity(fqdn), w.ses.Receiving(fqdn), w.ses.HasTenant(acct.TenantName))
	}
	if left := w.recordsUnder(probationDomain, fqdn); left != 0 {
		t.Fatalf("%d records of the released subdomain are still in the zone", left)
	}
	if _, err := w.accounts.GetByWorkspace(w.ctx, ws.ID); !errors.Is(err, es.ErrAccountNotFound) {
		t.Fatalf("account after the release: %v", err)
	}
	if senders, _ := w.senders.ListByWorkspace(w.ctx, ws.ID); len(senders) != 0 {
		t.Fatalf("senders left: %+v", senders)
	}
	if w.alerts.resolvedTimes(key) == 0 {
		t.Fatal("the warning episode stays open after the release")
	}
	status, err := w.module.Setup.Status(w.ctx, ws.ID)
	w.must(err)
	if status.Provisioned || status.Activation != nil {
		t.Fatalf("after the release the workspace sees Ativar e-mail again: %+v", status)
	}
	w.must(w.module.UnusedRelease.Sweep(w.ctx, now.Add(9*e2eDay)))

	rival := w.newTenant("Outra Loja", 0)
	_, err = w.module.Setup.Provision(w.ctx, rival.ID, "lojaesquecida")
	w.must(err)
	w.runSetup()
	if !w.ses.HasIdentity(fqdn) || w.recordsUnder(probationDomain, fqdn) == 0 {
		t.Fatal("another workspace could not take the freed name")
	}

	_, err = w.module.Setup.Provision(w.ctx, ws.ID, "")
	w.must(err)
	w.runSetup()
	if again := w.account(ws); again.Slug == "lojaesquecida" || again.Status != es.AccountActive || !w.ses.HasTenant(again.TenantName) {
		t.Fatalf("activating again = %+v", again)
	}
}

func TestEmailE2E_AnAccountThatSentIsNeverReleased(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Ativa", roomyWallet)
	lead := w.seedLeads(ws, 1, "ativa")[0]
	if _, err := w.sendFromWorkflow(ws, lead, "pedido:1"); err != nil {
		t.Fatal(err)
	}
	w.activatedAgo(ws, 60*e2eDay)
	now := time.Now().UTC()
	w.must(w.module.UnusedRelease.Sweep(w.ctx, now))
	w.must(w.module.UnusedRelease.Sweep(w.ctx, now.Add(30*e2eDay)))
	acct := w.account(ws)
	if acct.Status != es.AccountActive || acct.UnusedWarnedAt != nil || !w.ses.HasTenant(acct.TenantName) {
		t.Fatalf("an account that sent was touched: %+v", acct)
	}
	if w.alerts.publishedTimes("email_unused_release:"+ws.ID) != 0 {
		t.Fatal("an account that sent was warned")
	}
}
