package emailwiring_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
	"vozko/infra/ses/sesfake"
	balance_usecase "vozko/usecases/balance"
	"vozko/usecases/campaignqueue"
)

const roomyWallet = int64(20_000_000)

func (w *world) defaultUnit() int64 {
	w.t.Helper()
	q, err := w.billing.QuoteRun(w.ctx, uuid.NewString(), 1)
	w.must(err)
	return q.UnitMicros
}

func (w *world) assertDistinctSends(want int) {
	w.t.Helper()
	sent := w.ses.Sent()
	seen := map[emailaddress.Address]bool{}
	for _, s := range sent {
		if seen[s.Message.To] {
			w.t.Fatalf("%s was sent twice", s.Message.To)
		}
		seen[s.Message.To] = true
	}
	if len(sent) != want {
		w.t.Fatalf("provider sends = %d, want %d", len(sent), want)
	}
}

func TestEmailE2E_CampaignOfTwoThousandFiveHundredPaysThreeBatches(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Lotes", roomyWallet)
	unit := w.unit(ws)
	c := w.launch(ws, 2500)
	if debits := w.batchDebits(ws, c); len(debits) != 0 || w.wallet(ws) != ws.Start {
		t.Fatalf("launch charged %d batches, wallet %d: nothing is charged at launch", len(debits), w.wallet(ws))
	}

	w.dispatch(c, campaignWorkers, 0)

	final := w.campaign(c)
	if final.Status != emailcampaign.StatusCompleted || final.Settlement == nil || final.Settlement.RefundedUnits != 0 || final.Settlement.RefundedMicros != 0 {
		t.Fatalf("campaign = %s settlement %+v, want completed with nothing to refund", final.Status, final.Settlement)
	}
	if counts := w.counts(c); counts.Sent != 2500 || counts.Pending != 0 || counts.Failed != 0 || counts.SkippedTotal() != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	w.assertBatches(ws, c, 1000, 1000, 500)
	if slots := w.billingState(c).Slots; slots.Paid != 2500 || slots.Used != 2500 {
		t.Fatalf("slots = %+v, want sent = used = paid = 2500", slots)
	}
	if refunds := w.batchRefunds(ws, c); len(refunds) != 0 {
		t.Fatalf("refunds = %+v, want none", refunds)
	}
	if got, want := w.wallet(ws), ws.Start-2500*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertDistinctSends(2500)
	for _, s := range w.ses.Sent() {
		if s.Message.Kind != es.KindMarketing || s.Message.Tags["campaign_id"] != c.ID || !hasHeader(s.Message, "List-Unsubscribe") {
			t.Fatalf("send %+v is not a tagged marketing email with its unsubscribe header", s.Message.Tags)
		}
	}
	w.assertSlots(c)
	w.assertLedger(ws)
}

func hasHeader(m es.OutboundMessage, name string) bool {
	for _, h := range m.Headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

func TestEmailE2E_CampaignPausedInBatchTwoAndStoppedRefundsOnlyItsUnsentUnits(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Pausa", roomyWallet)
	unit := w.unit(ws)
	c := w.launch(ws, 2500)

	if sent := w.dispatch(c, 4, 1500); sent != 1500 {
		t.Fatalf("dispatched %d", sent)
	}
	if slots := w.billingState(c).Slots; slots.Paid != 2000 || slots.Used != 1500 {
		t.Fatalf("slots mid batch 2 = %+v", slots)
	}
	paused, err := w.campaigns.Service.Pause(w.ctx, ws.Actor, c.ID)
	w.must(err)
	if paused.Status != emailcampaign.StatusPaused {
		t.Fatalf("status = %s", paused.Status)
	}
	w.dispatch(c, 4, 0)
	if counts := w.counts(c); counts.Sent != 1500 || counts.Pending != 1000 {
		t.Fatalf("a paused campaign kept sending: %+v", counts)
	}
	if waiting := w.queue.delayedCount(campaignTopic(c)); waiting != 1000 {
		t.Fatalf("waiting while paused = %d", waiting)
	}
	if refunds := w.batchRefunds(ws, c); len(refunds) != 0 {
		t.Fatalf("a pause refunded %+v", refunds)
	}

	stopped, err := w.campaigns.Service.Stop(w.ctx, ws.Actor, c.ID)
	w.must(err)
	if stopped.Status != emailcampaign.StatusStopped || stopped.Settlement == nil || stopped.Settlement.RefundedUnits != 500 || stopped.Settlement.RefundedMicros != 500*unit {
		t.Fatalf("stopped = %s %+v, want 500 units refunded", stopped.Status, stopped.Settlement)
	}
	refunds := w.batchRefunds(ws, c)
	if len(refunds) != 1 || refunds[0].Amount != 500*unit || *refunds[0].ReferenceID != "refund:"+balance_usecase.EmailCampaignBatchReference(c.ID, 2) {
		t.Fatalf("refunds = %+v, want 500 units of batch 2", refunds)
	}
	state := w.billingState(c)
	if len(state.Batches) != 2 || state.Batches[1].RefundedUnits != 500 || state.Batches[0].RefundedUnits != 0 {
		t.Fatalf("batches = %+v", state.Batches)
	}
	w.assertBatches(ws, c, 1000, 1000)

	pending, err := w.recipientRepo.PendingIDs(w.ctx, c.ID, nil, "", 10)
	w.must(err)
	late, err := json.Marshal(campaignqueue.Message{CampaignID: c.ID, EntryID: pending[0]})
	w.must(err)
	w.must(w.queue.Publish(campaignTopic(c), late))
	w.dispatch(c, 1, 0)
	if counts := w.counts(c); counts.Sent != 1500 {
		t.Fatalf("a late redelivery after the stop sent: %+v", counts)
	}
	w.assertBatches(ws, c, 1000, 1000)
	if got, want := w.wallet(ws), ws.Start-1500*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertDistinctSends(1500)
	w.assertSlots(c)
	w.assertLedger(ws)
}

func TestEmailE2E_ShortWalletHoldsTheCampaignAtTheLastAffordableBatchAndResumesAfterATopUp(t *testing.T) {
	w := newWorld(t)
	unit := w.defaultUnit()
	ws := w.sendingTenant("Loja Curta", 1500*unit)
	c := w.launch(ws, 2500)

	w.dispatch(c, campaignWorkers, 0)

	held := w.campaign(c)
	if held.Status != emailcampaign.StatusHeld || held.HoldReason != emailcampaign.HoldBalanceShort {
		t.Fatalf("campaign = %s %q, want held for balance", held.Status, held.HoldReason)
	}
	if counts := w.counts(c); counts.Sent != 1000 || counts.Pending != 1500 {
		t.Fatalf("counts = %+v: batch 2 of 1000 costs more than the 500 sends left, so it is not bought", counts)
	}
	if got := w.wallet(ws); got != 500*unit {
		t.Fatalf("wallet = %d, want the 500 units left untouched", got)
	}
	w.assertBatches(ws, c, 1000)
	if state := w.billingState(c); len(state.Batches) != 2 || state.Batches[1].Charged() || state.Batches[1].UnitMicros != unit {
		t.Fatalf("batches = %+v, want batch 2 open at its stored price", state.Batches)
	}
	episode := "email_campaign_balance:" + c.ID
	if w.alerts.publishedTimes(episode) != 1 {
		t.Fatalf("balance episode published %d times", w.alerts.publishedTimes(episode))
	}
	if waiting := w.queue.delays(campaignTopic(c)); len(waiting) != 1500 || waiting[0] != 5*time.Second {
		t.Fatalf("waiting retries = %d, first after %v", len(waiting), waiting[0])
	}
	w.assertLedger(ws)

	w.topUp(ws, 2000*unit)
	w.queue.release(campaignTopic(c))
	w.dispatch(c, campaignWorkers, 0)

	final := w.campaign(c)
	if final.Status != emailcampaign.StatusCompleted || final.HoldReason != emailcampaign.HoldNone {
		t.Fatalf("campaign = %s %q, want completed", final.Status, final.HoldReason)
	}
	if counts := w.counts(c); counts.Sent != 2500 || counts.Pending != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	w.assertBatches(ws, c, 1000, 1000, 500)
	if w.alerts.resolvedTimes(episode) == 0 {
		t.Fatal("the balance episode was not resolved by the first send after the top up")
	}
	if got, want := w.wallet(ws), 1500*unit+2000*unit-2500*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertDistinctSends(2500)
	w.assertSlots(c)
	w.assertLedger(ws)
}

func TestEmailE2E_EachBatchIsChargedAndRefundedAtItsOwnStoredPrice(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Preços", roomyWallet)
	first := w.unit(ws)
	c := w.launch(ws, 2500)

	w.dispatch(c, 4, 1000)
	second := first + 117
	w.setSendPrice(second)
	w.dispatch(c, 4, 1000)
	third := second + 211
	w.setSendPrice(third)
	w.dispatch(c, 1, 1)
	w.setSendPrice(third + 50)

	debits := w.batchDebits(ws, c)
	if len(debits) != 3 || debits[0].Amount != 1000*first || debits[1].Amount != 1000*second || debits[2].Amount != 500*third {
		t.Fatalf("debits = %d %v, want 1000 at %d, 1000 at %d and 500 at %d", len(debits), amounts(debits), first, second, third)
	}
	_, err := w.campaigns.Service.Pause(w.ctx, ws.Actor, c.ID)
	w.must(err)
	stopped, err := w.campaigns.Service.Stop(w.ctx, ws.Actor, c.ID)
	w.must(err)
	refunds := w.batchRefunds(ws, c)
	if len(refunds) != 1 || refunds[0].Amount != 499*third || *refunds[0].ReferenceID != "refund:"+balance_usecase.EmailCampaignBatchReference(c.ID, 3) {
		t.Fatalf("refunds = %v, want 499 units of batch 3 at %d, not today's price", amounts(refunds), third)
	}
	if stopped.Settlement == nil || stopped.Settlement.RefundedUnits != 499 || stopped.Settlement.RefundedMicros != 499*third {
		t.Fatalf("settlement = %+v", stopped.Settlement)
	}
	if got, want := w.wallet(ws), ws.Start-1000*first-1000*second-third; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertDistinctSends(2001)
	w.assertSlots(c)
	w.assertLedger(ws)
}

func amounts(rows []schema.BalanceTransaction) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.Amount
	}
	return out
}

func TestEmailE2E_ThrottledCampaignRecipientsKeepTheirPaidSlotAndAreRetried(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Ritmo", roomyWallet)
	unit := w.unit(ws)
	c := w.launch(ws, 20)
	w.ses.SetQuota(es.SendQuota{MaxSendRate: 1, Max24HourSend: -1, ProductionAccess: true})
	w.must(w.module.SendQuota.Execute(w.ctx))

	w.dispatch(c, 1, 0)

	sentFirst := w.counts(c).Sent
	retries := w.queue.delays(campaignTopic(c))
	if len(retries) == 0 || sentFirst+int64(len(retries)) != 20 {
		t.Fatalf("sent %d and %d retries, want the throttled rest queued", sentFirst, len(retries))
	}
	for _, d := range retries {
		if d != time.Second {
			t.Fatalf("retry after %v, want the pacer hint floored at one second", d)
		}
	}
	if slots := w.billingState(c).Slots; slots.Paid != 20 || slots.Used != sentFirst {
		t.Fatalf("slots = %+v: a throttled recipient gives its slot back to the paid pool", slots)
	}
	w.assertBatches(ws, c, 20)

	w.ses.SetQuota(sesfake.UnlimitedQuota())
	w.must(w.module.SendQuota.Execute(w.ctx))
	w.queue.release(campaignTopic(c))
	w.dispatch(c, 1, 0)

	if final := w.campaign(c); final.Status != emailcampaign.StatusCompleted || final.Settlement == nil || final.Settlement.RefundedUnits != 0 {
		t.Fatalf("campaign = %s %+v", final.Status, final.Settlement)
	}
	w.assertBatches(ws, c, 20)
	if refunds := w.batchRefunds(ws, c); len(refunds) != 0 {
		t.Fatalf("a throttled recipient was refunded: %v", amounts(refunds))
	}
	if got, want := w.wallet(ws), ws.Start-20*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertDistinctSends(20)
	w.assertSlots(c)
	w.assertLedger(ws)
}
