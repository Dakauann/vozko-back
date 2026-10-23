package emailwiring_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	es "vozko/domain/emailsending"
	"vozko/usecases/transactionalemail"
)

func (w *world) account(ws *tenant) es.SenderAccount {
	w.t.Helper()
	acct, err := w.accounts.GetByWorkspace(w.ctx, ws.ID)
	w.must(err)
	return *acct
}

func TestEmailE2E_AnIdleTenantIsRetiredAndConcurrentSendsReviveItOnce(t *testing.T) {
	w := newWorld(t)
	idle := w.sendingTenant("Loja Parada", roomyWallet)
	busy := w.sendingTenant("Loja Ocupada", roomyWallet)
	unit := w.unit(idle)
	people := w.seedLeads(idle, 8, "parada")
	live := w.launch(busy, 1)
	tenantName := w.account(idle).TenantName

	later := time.Now().Add(es.DormancyAfter + 24*time.Hour)
	w.must(w.module.Dormancy.Sweep(w.ctx, later))

	if acct := w.account(idle); acct.Status != es.AccountDormant || w.ses.HasTenant(tenantName) {
		t.Fatalf("idle account = %s, tenant kept %v: an idle tenant is retired", acct.Status, w.ses.HasTenant(tenantName))
	}
	if acct := w.account(busy); acct.Status != es.AccountActive || !w.ses.HasTenant(acct.TenantName) || w.campaign(live).Status != emailcampaign.StatusRunning {
		t.Fatalf("an account with a running campaign was retired: %s", acct.Status)
	}
	if w.ses.Retirements() != 1 {
		t.Fatalf("retirements = %d", w.ses.Retirements())
	}
	w.must(w.module.Dormancy.Sweep(w.ctx, later))
	if w.ses.Retirements() != 1 {
		t.Fatalf("a second sweep retired again: %d", w.ses.Retirements())
	}

	w.ses.SlowRevive(300 * time.Millisecond)
	results := make([]transactionalemail.Result, len(people))
	errs := make([]error, len(people))
	var wg sync.WaitGroup
	for i := range people {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = w.sendFromWorkflow(idle, people[i], fmt.Sprintf("volta:%d", i))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || results[i].ConversationID == "" {
			t.Fatalf("send %d after the retirement = %+v %v", i, results[i], err)
		}
	}
	if w.ses.Revivals() != 1 {
		t.Fatalf("revivals = %d, want concurrent sends to rebuild the tenant once", w.ses.Revivals())
	}
	if w.ses.SendsToMissingTenants() != 0 {
		t.Fatalf("%d sends reached the provider before the tenant was back", w.ses.SendsToMissingTenants())
	}
	if acct := w.account(idle); acct.Status != es.AccountActive || !w.ses.HasTenant(tenantName) {
		t.Fatalf("after the revive the account is %s", acct.Status)
	}
	for _, lead := range people {
		w.onlySentTo(emailaddress.Address(lead.Email))
	}
	debits, refunds := w.oneToOneCharges(idle)
	if len(debits) != len(people) || len(refunds) != 0 {
		t.Fatalf("debits %d refunds %d", len(debits), len(refunds))
	}
	if got, want := w.wallet(idle), idle.Start-int64(len(people))*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	if w.conversationsOf(idle) != int64(len(people)) {
		t.Fatalf("conversations = %d", w.conversationsOf(idle))
	}
	w.assertLedger(idle)
}
