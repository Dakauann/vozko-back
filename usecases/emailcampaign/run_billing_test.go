package emailcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/usecases/campaignqueue"
)

const startWallet = int64(20_000_000)

type billedRun struct {
	t    *testing.T
	w    *world
	c    emailcampaign.Campaign
	unit int64
}

func crowd(n int) []person {
	people := make([]person, n)
	for i := range people {
		people[i] = person{email: fmt.Sprintf("p%05d@x.com", i), subscribed: true}
	}
	return people
}

func launchBilled(t *testing.T, recipients int, wallet int64) *billedRun {
	t.Helper()
	w := newWorld(t)
	w.billing.balance = wallet
	*w.wallet = wallet
	w.audience(crowd(recipients)...)
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	return &billedRun{t: t, w: w, c: w.campaign(c.ID), unit: w.billing.unit}
}

func (r *billedRun) pendingIDs() []string {
	var ids []string
	for _, rc := range r.w.recips.byAddress(r.c.ID) {
		if rc.Status == emailcampaign.RecipientPending {
			ids = append(ids, rc.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func (r *billedRun) deliver(n int) []campaignqueue.Result {
	r.t.Helper()
	ids := r.pendingIDs()
	if n > len(ids) {
		n = len(ids)
	}
	out := make([]campaignqueue.Result, 0, n)
	for _, id := range ids[:n] {
		out = append(out, r.w.svc.handle(campaignqueue.Message{CampaignID: r.c.ID, EntryID: id}))
	}
	return out
}

func (r *billedRun) sent() int64 {
	counts, err := r.w.recips.Counts(context.Background(), r.c.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	return counts.Sent
}

func (r *billedRun) slots() emailcampaign.SlotCounters {
	return r.w.batches.state(r.c.ID).Slots
}

func (r *billedRun) charges() []string { return r.w.billing.charges() }

func (r *billedRun) batch(no int) emailcampaign.ChargeBatch {
	r.t.Helper()
	for _, b := range r.w.batches.state(r.c.ID).Batches {
		if b.No == no {
			return b
		}
	}
	r.t.Fatalf("batch %d does not exist", no)
	return emailcampaign.ChargeBatch{}
}

func (r *billedRun) pause() {
	r.t.Helper()
	if _, err := r.w.svc.Pause(context.Background(), r.w.actor(), r.c.ID); err != nil {
		r.t.Fatal(err)
	}
}

func (r *billedRun) resume() {
	r.t.Helper()
	if _, err := r.w.svc.Resume(context.Background(), r.w.actor(), r.c.ID); err != nil {
		r.t.Fatal(err)
	}
}

func (r *billedRun) stop() {
	r.t.Helper()
	if _, err := r.w.svc.Stop(context.Background(), r.w.actor(), r.c.ID); err != nil {
		r.t.Fatal(err)
	}
}

func (r *billedRun) complete() {
	r.t.Helper()
	done, err := r.w.svc.completeIfDone(context.Background(), r.c.ID)
	if err != nil || !done {
		r.t.Fatalf("complete = %v, %v", done, err)
	}
}

func countOutcomes(results []campaignqueue.Result, outcome campaignqueue.Outcome) int {
	n := 0
	for _, res := range results {
		if res.Outcome == outcome {
			n++
		}
	}
	return n
}

func batchKeys(campaignID string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = batchKey(campaignID, i+1)
	}
	return keys
}

func TestTheFirstBatchIsChargedBeforeTheFirstSend(t *testing.T) {
	r := launchBilled(t, 3, startWallet)
	if len(r.charges()) != 0 {
		t.Fatalf("nothing is charged at launch: %v", r.charges())
	}
	r.deliver(1)
	entries := r.w.journal.all()
	charged := slices.Index(entries, "charge batch "+batchKey(r.c.ID, 1))
	sentAt := slices.IndexFunc(entries, func(e string) bool { return len(e) > 5 && e[:5] == "send " })
	if charged < 0 || sentAt < 0 || charged > sentAt {
		t.Fatalf("journal = %v, want the first batch charged before the first send", entries)
	}
	if b := r.batch(1); b.Units != 3 || b.UnitMicros != r.unit {
		t.Fatalf("the first batch covers only the run: %+v", b)
	}
}

func TestARunOf2500IsChargedInBatchesOf1000And1000And500(t *testing.T) {
	r := launchBilled(t, 2500, startWallet)
	if done := countOutcomes(r.deliver(2500), campaignqueue.OutcomeDone); done != 2500 {
		t.Fatalf("done = %d", done)
	}
	if got := r.charges(); !slices.Equal(got, batchKeys(r.c.ID, 3)) {
		t.Fatalf("charges = %v", got)
	}
	for no, units := range map[int]int64{1: 1000, 2: 1000, 3: 500} {
		if b := r.batch(no); b.Units != units || !b.Charged() {
			t.Fatalf("batch %d = %+v, want %d units", no, b, units)
		}
	}
	r.complete()
	if got := r.w.billing.wallet(); got != startWallet-2500*r.unit {
		t.Fatalf("wallet = %d, want %d", got, startWallet-2500*r.unit)
	}
	if len(r.w.billing.batchRefunds) != 0 {
		t.Fatalf("everything was sent, nothing to refund: %+v", r.w.billing.batchRefunds)
	}
}

func TestAShortWalletHoldsWithoutSendingAndResumesAfterATopUp(t *testing.T) {
	r := launchBilled(t, 1500, 1000*583+100)
	results := r.deliver(1500)
	if countOutcomes(results, campaignqueue.OutcomeDone) != 1000 || len(r.w.gateway.calls) != 1000 {
		t.Fatalf("sent %d, want exactly the 1000 paid", len(r.w.gateway.calls))
	}
	last := results[len(results)-1]
	if last.Outcome != campaignqueue.OutcomeRetryLater || last.Delay != balanceRetryDelay {
		t.Fatalf("a recipient without a paid slot waits for the wallet: %+v", last)
	}
	held := r.w.campaign(r.c.ID)
	if held.Status != emailcampaign.StatusHeld || held.HoldReason != emailcampaign.HoldBalanceShort {
		t.Fatalf("campaign = %s/%s", held.Status, held.HoldReason)
	}
	notes := r.w.notes.drafts
	if len(notes) != 1 || notes[0].EpisodeKey != balanceHoldEpisode(r.c.ID) || notes[0].DedupKey != "" {
		t.Fatalf("notices = %+v, want one episode for the campaign", notes)
	}
	if got := r.charges(); !slices.Equal(got, batchKeys(r.c.ID, 1)) {
		t.Fatalf("charges = %v", got)
	}
	r.w.billing.topUp(startWallet)
	if done := countOutcomes(r.deliver(500), campaignqueue.OutcomeDone); done != 500 {
		t.Fatalf("after the top up %d were sent, want 500", done)
	}
	resumed := r.w.campaign(r.c.ID)
	if resumed.Status != emailcampaign.StatusRunning || resumed.HoldReason != emailcampaign.HoldNone {
		t.Fatalf("campaign = %s/%s", resumed.Status, resumed.HoldReason)
	}
	if !slices.Contains(r.w.notes.resolved, balanceHoldEpisode(r.c.ID)) {
		t.Fatalf("resolved = %v, want the balance episode closed", r.w.notes.resolved)
	}
	if b := r.batch(2); b.Units != 500 || !b.Charged() {
		t.Fatalf("batch 2 = %+v", b)
	}
}

func pausedAt1400(t *testing.T) *billedRun {
	t.Helper()
	r := launchBilled(t, 3000, startWallet)
	r.deliver(1400)
	r.pause()
	return r
}

func TestPauseKeepsThePaidSlotsAndChargesNothing(t *testing.T) {
	r := pausedAt1400(t)
	if r.sent() != 1400 {
		t.Fatalf("sent = %d", r.sent())
	}
	if got := r.slots(); got.Paid != 2000 || got.Used != 1400 {
		t.Fatalf("slots = %+v, want 2000 paid and 1400 used", got)
	}
	results := r.deliver(800)
	if countOutcomes(results, campaignqueue.OutcomeRetryLater) != 800 || len(r.w.gateway.calls) != 1400 {
		t.Fatal("nothing is sent while paused")
	}
	if got := r.charges(); !slices.Equal(got, batchKeys(r.c.ID, 2)) || len(r.w.billing.batchRefunds) != 0 {
		t.Fatalf("charges = %v refunds = %+v, want no charge and no refund on pause", got, r.w.billing.batchRefunds)
	}
	if got := r.w.billing.wallet(); got != startWallet-2000*r.unit {
		t.Fatalf("wallet = %d", got)
	}
}

func TestResumeUsesTheRemainingSlotsBeforeTheNextBatch(t *testing.T) {
	r := pausedAt1400(t)
	r.resume()
	r.deliver(600)
	if got := r.charges(); !slices.Equal(got, batchKeys(r.c.ID, 2)) {
		t.Fatalf("the 600 remaining slots of batch 2 need no new debit: %v", got)
	}
	r.deliver(1)
	if got := r.charges(); !slices.Equal(got, batchKeys(r.c.ID, 3)) {
		t.Fatalf("the 2001st send charges batch 3: %v", got)
	}
	r.deliver(999)
	r.complete()
	if got := r.charges(); len(got) != 3 {
		t.Fatalf("charges = %v, want 3 batches", got)
	}
	if got := r.w.billing.wallet(); got != startWallet-3000*r.unit {
		t.Fatalf("wallet = %d, want %d", got, startWallet-3000*r.unit)
	}
}

func TestStopWhilePausedRefundsTheUnusedSlotsOfTheNewestBatchOnce(t *testing.T) {
	r := pausedAt1400(t)
	r.stop()
	refunds := r.w.billing.batchRefunds
	if len(refunds) != 1 || refunds[0].key != batchKey(r.c.ID, 2) || refunds[0].units != 600 || refunds[0].unitMicros != r.batch(2).UnitMicros {
		t.Fatalf("refunds = %+v, want 600 of batch 2 at its stored price", refunds)
	}
	if got := r.w.billing.wallet(); got != startWallet-1400*r.unit {
		t.Fatalf("wallet = %d, want %d", got, startWallet-1400*r.unit)
	}
	settled := r.w.campaign(r.c.ID)
	if settled.Settlement == nil || settled.Settlement.RefundedUnits != 600 || settled.Settlement.RefundedMicros != 600*r.unit {
		t.Fatalf("settlement = %+v", settled.Settlement)
	}
	if b := r.batch(2); b.RefundedUnits != 600 || !b.Refunded() {
		t.Fatalf("batch 2 = %+v, want its refund recorded", b)
	}
	if _, err := r.w.svc.Stop(context.Background(), r.w.actor(), r.c.ID); !errors.Is(err, emailcampaign.ErrFinished) {
		t.Fatalf("a second stop = %v", err)
	}
	late := r.deliver(1)
	if late[0].Outcome != campaignqueue.OutcomeDrop || len(r.w.gateway.calls) != 1400 {
		t.Fatalf("a late redelivery after the stop = %+v, sends %d", late[0], len(r.w.gateway.calls))
	}
	if err := r.w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.w.billing.batchRefunds) != 1 || len(r.charges()) != 2 {
		t.Fatalf("nothing more is charged or refunded: charges %v refunds %+v", r.charges(), r.w.billing.batchRefunds)
	}
}

func TestRecipientsRefusedAtSendReleaseTheirSlotsAndAreRefundedAtCompletion(t *testing.T) {
	r := pausedAt1400(t)
	pending := r.pendingIDs()
	refused := map[emailaddress.Address]error{}
	for _, id := range pending[len(pending)-50:] {
		rc, _ := r.w.recips.Get(context.Background(), id)
		refused[rc.Address] = emailsending.ErrRecipientSuppressed
	}
	r.w.gateway.byAddress = refused
	r.resume()
	r.deliver(1600)
	if got := r.slots(); got.Paid != 3000 || got.Used != 2950 {
		t.Fatalf("slots = %+v, want the 50 refused slots given back", got)
	}
	r.complete()
	refunds := r.w.billing.batchRefunds
	if len(refunds) != 1 || refunds[0].key != batchKey(r.c.ID, 3) || refunds[0].units != 50 {
		t.Fatalf("refunds = %+v, want 50 of batch 3", refunds)
	}
	if got := r.w.billing.wallet(); got != startWallet-2950*r.unit {
		t.Fatalf("wallet = %d, want %d", got, startWallet-2950*r.unit)
	}
}

func TestAPriceChangeKeepsEachBatchAtItsOwnPrice(t *testing.T) {
	r := pausedAt1400(t)
	oldUnit, newUnit := r.unit, r.unit+117
	r.w.billing.setUnit(newUnit)
	r.resume()
	r.deliver(600)
	if len(r.charges()) != 2 {
		t.Fatalf("batch 2's slots stay at the old price, no new debit: %v", r.charges())
	}
	r.deliver(1)
	if b := r.batch(3); b.UnitMicros != newUnit || b.Units != 1000 {
		t.Fatalf("batch 3 = %+v, want the new price", b)
	}
	if b := r.batch(2); b.UnitMicros != oldUnit {
		t.Fatalf("batch 2 = %+v, want the old price", b)
	}
	r.stop()
	refunds := r.w.billing.batchRefunds
	if len(refunds) != 1 || refunds[0].key != batchKey(r.c.ID, 3) || refunds[0].units != 999 || refunds[0].unitMicros != newUnit {
		t.Fatalf("refunds = %+v, want 999 of batch 3 at the new price", refunds)
	}
	if got, want := r.w.billing.wallet(), startWallet-2000*oldUnit-newUnit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
}

func TestASkippedRecipientGivesItsSlotBack(t *testing.T) {
	r := launchBilled(t, 2, startWallet)
	ids := r.pendingIDs()
	first, _ := r.w.recips.Get(context.Background(), ids[0])
	r.w.gateway.byAddress = map[emailaddress.Address]error{first.Address: emailsending.ErrRecipientSuppressed}
	r.deliver(1)
	if got := r.slots(); got.Paid != 2 || got.Used != 0 {
		t.Fatalf("slots = %+v, want the skipped slot given back", got)
	}
	r.deliver(1)
	r.complete()
	if refunds := r.w.billing.batchRefunds; len(refunds) != 1 || refunds[0].units != 1 {
		t.Fatalf("refunds = %+v", refunds)
	}
}

func TestARetriedSendGivesItsSlotBackUntilItIsSent(t *testing.T) {
	r := launchBilled(t, 1, startWallet)
	r.w.gateway.err = &emailsending.SendError{Kind: emailsending.SendErrorTransient}
	if res := r.deliver(1); res[0].Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("outcome = %+v", res[0])
	}
	if got := r.slots(); got.Used != 0 {
		t.Fatalf("slots = %+v, a retried recipient holds no slot", got)
	}
	r.w.gateway.err = nil
	r.deliver(1)
	if got := r.slots(); got.Used != 1 || got.Paid != 1 || len(r.charges()) != 1 {
		t.Fatalf("slots = %+v charges %v", got, r.charges())
	}
}

func TestARedeliveredRecipientReusesTheSlotItAlreadyHolds(t *testing.T) {
	r := launchBilled(t, 2, startWallet)
	ids := r.pendingIDs()
	if err := r.w.svc.billing.Claim(context.Background(), r.c, ids[0]); err != nil {
		t.Fatal(err)
	}
	r.deliver(2)
	if got := r.slots(); got.Used != 2 || got.Paid != 2 || r.sent() != 2 {
		t.Fatalf("slots = %+v sent %d, want one slot per recipient", got, r.sent())
	}
	if len(r.charges()) != 1 {
		t.Fatalf("charges = %v", r.charges())
	}
}

func TestStopWaitsForASendInFlightBeforeSettling(t *testing.T) {
	r := launchBilled(t, 2, startWallet)
	ids := r.pendingIDs()
	if err := r.w.svc.billing.Claim(context.Background(), r.c, ids[0]); err != nil {
		t.Fatal(err)
	}
	r.stop()
	if got := r.w.campaign(r.c.ID); got.Settlement != nil || len(r.w.billing.batchRefunds) != 0 {
		t.Fatalf("a send in flight may still go out, so the stop waits: %+v", got.Settlement)
	}
	if err := r.w.svc.billing.Release(context.Background(), r.c, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := r.w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if refunds := r.w.billing.batchRefunds; len(refunds) != 1 || refunds[0].units != 2 {
		t.Fatalf("refunds = %+v, want both unsent units", refunds)
	}
}

func TestAStaleClaimCountsAsSentSoNothingUnpaidCanGoOut(t *testing.T) {
	r := launchBilled(t, 2, startWallet)
	ids := r.pendingIDs()
	if err := r.w.svc.billing.Claim(context.Background(), r.c, ids[0]); err != nil {
		t.Fatal(err)
	}
	r.stop()
	r.w.svc.deps.Now = func() time.Time { return now.Add(claimStaleAfter + time.Second) }
	if err := r.w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if refunds := r.w.billing.batchRefunds; len(refunds) != 1 || refunds[0].units != 1 {
		t.Fatalf("refunds = %+v, want only the unclaimed unit", refunds)
	}
}

func TestReviewAsksTheWalletOnlyForTheFirstBatch(t *testing.T) {
	w := newWorld(t)
	w.audience(crowd(1500)...)
	c := w.draft(nil)
	*w.wallet = 1000 * 583
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Send.Units != 1500 || r.Send.TotalMicros != 1500*583 || r.FirstBatch.Units != 1000 || r.FirstBatch.TotalMicros != 1000*583 || r.BatchSize != emailcampaign.BillingBatchSize {
		t.Fatalf("review = %+v first %+v", r.Send, r.FirstBatch)
	}
	if r.MissingMicros != 0 || len(r.Check.Blocking) != 0 {
		t.Fatalf("a wallet covering the first batch launches: %+v", r.Check.Blocking)
	}
}

func TestConcurrentSendersWithRedeliveriesNeverSendUnpaid(t *testing.T) {
	const recipients, workers = 5000, 32
	r := launchBilled(t, recipients, startWallet*10)
	var violations atomic.Int64
	r.w.gateway.onSend = func(sentBefore int) {
		if int64(sentBefore+1) > r.w.batches.state(r.c.ID).Slots.Paid {
			violations.Add(1)
		}
	}
	queue := make(chan string, recipients*2)
	for _, id := range r.pendingIDs() {
		queue <- id
	}
	var crashed sync.Map
	var outstanding atomic.Int64
	outstanding.Store(recipients)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for outstanding.Load() > 0 {
				var id string
				select {
				case id = <-queue:
				default:
					runtime.Gosched()
					continue
				}
				if _, again := crashed.LoadOrStore(id, true); !again && id[len(id)-1] == '7' {
					_ = r.w.svc.billing.Claim(context.Background(), r.c, id)
					queue <- id
					continue
				}
				res := r.w.svc.handle(campaignqueue.Message{CampaignID: r.c.ID, EntryID: id})
				if res.Outcome == campaignqueue.OutcomeRetryLater {
					queue <- id
					continue
				}
				outstanding.Add(-1)
			}
		}()
	}
	wg.Wait()
	if violations.Load() != 0 {
		t.Fatalf("%d sends went out without a paid slot", violations.Load())
	}
	if r.sent() != recipients {
		t.Fatalf("sent = %d", r.sent())
	}
	if got := r.charges(); len(got) != recipients/int(emailcampaign.BillingBatchSize) {
		t.Fatalf("charges = %v, want exactly %d batches", got, recipients/int(emailcampaign.BillingBatchSize))
	}
	if got := r.slots(); got.Used != recipients || got.Paid != recipients {
		t.Fatalf("slots = %+v", got)
	}
}

func TestAPacedSendRetriesAfterTheHintWithoutRefundOrNewCharge(t *testing.T) {
	r := launchBilled(t, 2, startWallet)
	r.w.gateway.err = &emailsending.SendError{Kind: emailsending.SendErrorTransient, RetryAfter: emailsending.DailyQuotaRetryAfter}
	res := r.deliver(1)
	if res[0].Outcome != campaignqueue.OutcomeRetryLater || res[0].Delay != emailsending.DailyQuotaRetryAfter {
		t.Fatalf("outcome = %+v, want a retry after the pacer hint", res[0])
	}
	if got := r.slots(); got.Paid != 2 || got.Used != 0 {
		t.Fatalf("slots = %+v, want the slot back in the paid pool for the retry", got)
	}
	if len(r.charges()) != 1 || len(r.w.billing.batchRefunds) != 0 {
		t.Fatalf("charges %v refunds %+v, want no refund and no new charge", r.charges(), r.w.billing.batchRefunds)
	}
	r.w.gateway.err = nil
	r.deliver(2)
	if r.sent() != 2 || len(r.charges()) != 1 || len(r.w.billing.batchRefunds) != 0 || r.w.billing.wallet() != startWallet-2*r.unit {
		t.Fatalf("sent %d charges %v refunds %+v wallet %d", r.sent(), r.charges(), r.w.billing.batchRefunds, r.w.billing.wallet())
	}
}

func TestATransientFailureRetryDelay(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"no hint keeps the usual delay", &emailsending.SendError{Kind: emailsending.SendErrorTransient}, transientRetryDelay},
		{"a quota hint waits the quota out", &emailsending.SendError{Kind: emailsending.SendErrorTransient, RetryAfter: 15 * time.Minute}, 15 * time.Minute},
		{"a rate hint retries the next second", &emailsending.SendError{Kind: emailsending.SendErrorTransient, RetryAfter: time.Second}, time.Second},
		{"a hint below the floor waits the floor", &emailsending.SendError{Kind: emailsending.SendErrorTransient, RetryAfter: time.Millisecond}, retryHintFloor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := launchBilled(t, 1, startWallet)
			r.w.gateway.err = tc.err
			if res := r.deliver(1); res[0].Outcome != campaignqueue.OutcomeRetryLater || res[0].Delay != tc.want {
				t.Fatalf("outcome = %+v, want a retry after %v", res[0], tc.want)
			}
		})
	}
}
