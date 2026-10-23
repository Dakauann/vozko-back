package emailwiring_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	"vozko/domain/selection"
	"vozko/infra/database/schema"
	"vozko/infra/ses/sesfake"
	"vozko/usecases/transactionalemail"
)

func (w *world) seedNewLeads(ws *tenant, n int, label string) []schema.Lead {
	w.t.Helper()
	leads := make([]schema.Lead, n)
	for i := range leads {
		leads[i] = schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: fmt.Sprintf("Novo %s %d", label, i),
			Email: schema.OptionalText(fmt.Sprintf("%s%05d@%s.example", label, i, ws.ID[:8]))}
	}
	w.must(w.db.CreateInBatches(leads, 500).Error)
	return leads
}

func (w *world) draftToEveryone(ws *tenant) emailcampaign.Campaign {
	w.t.Helper()
	everyone := selection.Selection{Mode: selection.ModeEveryone}
	draft, err := w.campaigns.Service.Create(w.ctx, ws.Actor, emailcampaign.Draft{
		Name: "Higiene", Subject: "Oferta para {{first_name|você}}", Document: readyDocument(), Selection: &everyone,
	})
	w.must(err)
	return *draft
}

func TestEmailE2E_AHardBounceForOneCustomerBlocksTheAddressForEveryWorkspace(t *testing.T) {
	w := newWorld(t)
	first := w.sendingTenant("Loja Um", roomyWallet)
	second := w.sendingTenant("Loja Dois", roomyWallet)
	gone := w.seedLeads(first, 1, "sumiu")[0]
	res, err := w.sendFromWorkflow(first, gone, "pedido:"+gone.ID)
	w.must(err)
	if res.MessageID == "" {
		t.Fatalf("first send = %+v", res)
	}
	w.providerEvent(sesfake.HardBounce, w.onlySentTo(emailaddress.Address(gone.Email)))
	var shared int64
	w.must(w.db.Raw("SELECT count(*) FROM email_bad_addresses WHERE address_hash = sha256(convert_to(?, 'UTF8'))", string(gone.Email)).Scan(&shared).Error)
	if shared != 1 {
		t.Fatalf("shared rows = %d, a hard bounce reaches the shared list once", shared)
	}

	same := schema.Lead{ID: uuid.NewString(), WorkspaceID: second.ID, Name: "Mesma pessoa", Email: gone.Email}
	w.must(w.db.Create(&same).Error)
	w.mailedBefore(second, []schema.Lead{same})
	_, err = w.sendFromWorkflow(second, same, "pedido:"+same.ID)
	var refusal *transactionalemail.Refusal
	if !errors.As(err, &refusal) || refusal.Code != transactionalemail.CodeRecipientSuppressed {
		t.Fatalf("err = %v, the second workspace never mails an address that does not exist", err)
	}
	if sent := w.ses.SentTo(emailaddress.Address(gone.Email)); len(sent) != 1 {
		t.Fatalf("the provider got %d sends, only the first one", len(sent))
	}
	w.seedLeads(second, 2, "ok")
	review, err := w.campaigns.Service.Review(w.ctx, second.Actor, w.draftToEveryone(second).ID)
	w.must(err)
	if review.Audience.Skipped[emailcampaign.SkipBadAddress] != 1 || review.Audience.Sendable != 2 {
		t.Fatalf("audience = %+v", review.Audience)
	}
	suppressions, _, err := w.suppressions.List(w.ctx, second.ID, es.SuppressionQuery{Limit: 10})
	w.must(err)
	if len(suppressions) != 0 {
		t.Fatalf("the second workspace's own list stays clean, got %+v", suppressions)
	}
}

func TestEmailE2E_TheReviewSkipsDeadDomainsAndDisposableAddressesForFree(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Limpa", roomyWallet)
	w.seedLeads(ws, 3, "bom")
	w.resolver.set("semcorreio.example", h.DNSAnswer{MX: h.LookupFound, MXHosts: []string{"."}})
	for _, email := range []string{"ana@semcorreio.example", "bia@mailinator.com"} {
		l := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: email, Email: schema.OptionalText(email)}
		w.must(w.db.Create(&l).Error)
		w.mailedBefore(ws, []schema.Lead{l})
	}
	before := len(w.ses.Sent())
	review, err := w.campaigns.Service.Review(w.ctx, ws.Actor, w.draftToEveryone(ws).ID)
	w.must(err)
	if review.Audience.Sendable != 3 || review.Audience.Skipped[emailcampaign.SkipNoMailDomain] != 1 || review.Audience.Skipped[emailcampaign.SkipDisposable] != 1 {
		t.Fatalf("audience = %+v", review.Audience)
	}
	if len(w.ses.Sent()) != before {
		t.Fatal("a review never sends anything")
	}
	if n := w.resolver.lookups["semcorreio.example"]; n != 1 {
		t.Fatalf("semcorreio.example looked up %d times, once per domain", n)
	}
	if _, err := w.campaigns.Service.Review(w.ctx, ws.Actor, w.draftToEveryone(ws).ID); err != nil {
		t.Fatal(err)
	}
	if n := w.resolver.lookups["semcorreio.example"]; n != 1 {
		t.Fatalf("a second review looked the domain up again (%d), the cache keeps it", n)
	}
}

func TestEmailE2E_ANewListSendsItsTestBatchAndStopsWhenItBounces(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Lista Comprada", roomyWallet)
	w.seedNewLeads(ws, 1000, "novo")
	c := w.draftToEveryone(ws)
	review, err := w.campaigns.Service.Review(w.ctx, ws.Actor, c.ID)
	w.must(err)
	if review.Audience.NeverMailed != 1000 || review.TestBatch != 200 {
		t.Fatalf("review never mailed %d test batch %d", review.Audience.NeverMailed, review.TestBatch)
	}
	launched, err := w.campaigns.Service.Launch(w.ctx, ws.Actor, c.ID, nil)
	w.must(err)
	if launched.TestBatch == nil || launched.TestBatch.Size != 200 {
		t.Fatalf("test batch = %+v", launched.TestBatch)
	}
	w.dispatch(*launched, campaignWorkers, 0)
	sent := w.ses.Sent()
	if len(sent) != 200 || w.counts(*launched).Pending != 800 {
		t.Fatalf("sent %d with %d pending, only the test batch goes out", len(sent), w.counts(*launched).Pending)
	}
	for i, s := range sent {
		if i < 7 {
			w.providerEvent(sesfake.HardBounce, s)
			continue
		}
		w.providerEvent(sesfake.Delivery, s)
	}
	w.must(w.campaigns.Service.Tick(w.ctx))
	held := w.campaign(*launched)
	if held.Status != emailcampaign.StatusHeld || held.HoldReason != emailcampaign.HoldTestBatchBounces || held.TestBatch.State != emailcampaign.TestBatchFailed {
		t.Fatalf("campaign = %s %s %+v", held.Status, held.HoldReason, held.TestBatch)
	}
	if _, err := w.campaigns.Service.Resume(w.ctx, ws.Actor, c.ID); !errors.Is(err, emailcampaign.ErrTestBatchFailed) {
		t.Fatalf("resume err = %v", err)
	}
	failed, err := w.campaignRepo.FailedTestBatches(w.ctx, ws.ID, held.CreatedAt.AddDate(0, 0, -1))
	w.must(err)
	if failed != 1 {
		t.Fatalf("failed test batches = %d, the promotion sweep reads it", failed)
	}
	if _, err := w.campaigns.Service.Stop(w.ctx, ws.Actor, c.ID); err != nil {
		t.Fatal(err)
	}
	if len(w.ses.Sent()) != 200 {
		t.Fatal("nothing more was sent after the failed test batch")
	}
	state := w.billingState(*launched)
	if state.Slots.Used != 200 {
		t.Fatalf("used = %d", state.Slots.Used)
	}
	w.assertBatches(ws, *launched, 1000)
	if refunds := w.batchRefunds(ws, *launched); len(refunds) != 1 || refunds[0].Amount != 800*state.Batches[0].UnitMicros {
		t.Fatalf("refunds = %+v, a stop gives back the 800 emails that never left", refunds)
	}
	w.assertLedger(ws)
}
