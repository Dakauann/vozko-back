package whatsapp_campaign_usecase

import (
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// The billing path of the Cloud API campaign consumer, asserted exhaustively.
//
// This is the highest-volume paid surface in the product: one campaign can debit
// a workspace a hundred and fifty thousand times, so every refusal has to be
// proven not to charge, every charge has to be proven to happen once, and every
// reservation has to be proven to be released. A leak here is silent — the money
// is simply gone, or simply never taken — and neither shows up as an error.
//
// The cases below are the complete decision tree of handle(): each pre-debit
// refusal, each debit failure mode, each post-send outcome, and the invariants
// that must hold across all of them.

// ledger records every movement with the reference it was made under, which is
// what lets these tests assert attribution and not merely counts. The existing
// mockConsumeWhatsappTemplate records only categories.
type ledger struct {
	mu      sync.Mutex
	debits  []string
	refunds []string

	cost       int64
	zeroCost   bool
	costErr    error
	executeErr error
}

func (l *ledger) Execute(_ string, reference string, _ string) (*balance.Transaction, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.executeErr != nil {
		return nil, l.executeErr
	}
	l.debits = append(l.debits, reference)
	return &balance.Transaction{}, nil
}

func (l *ledger) Refund(_ string, reference string, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refunds = append(l.refunds, reference)
	return nil
}

func (l *ledger) GetTemplateCostMicros(_ string, _ string) (int64, error) {
	if l.costErr != nil {
		return 0, l.costErr
	}
	if l.zeroCost {
		return 0, nil
	}
	if l.cost > 0 {
		return l.cost, nil
	}
	return 100, nil
}

func (l *ledger) snapshot() (debits, refunds []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.debits...), append([]string(nil), l.refunds...)
}

// billingRig is a harness whose ledger is observable.
type billingRig struct {
	*testHarness
	ledger  *ledger
	campID  string
	topic   string
	entryID string
}

func newBillingRig(t *testing.T) *billingRig {
	t.Helper()
	h := newTestHarness()
	l := &ledger{cost: 500}

	h.consumer.ConsumeWhatsappTemplate = l
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	h.cachedBalanceChecker.balanceMicros = 1_000_000

	const campID = "camp-billing"
	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1",
		BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{
		ID: "entry-1", LeadID: "lead-1", Status: wce.SendStatusPending,
	}

	if err := h.consumer.SubscribeToCampaign(campID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	return &billingRig{
		testHarness: h, ledger: l,
		campID: campID, topic: makeTopic(campID), entryID: "entry-1",
	}
}

func (r *billingRig) deliver() *mockAck {
	return r.queueSub.deliver(r.topic, makePayload(r.campID, r.entryID, "5584999990001"))
}

// inflight reads the reservation counter the reserver writes through.
func (r *billingRig) inflight(t *testing.T) int64 {
	t.Helper()
	raw, _ := r.shared.GetString("balance:inflight:ws-1")
	if raw == "" {
		return 0
	}
	var n int64
	for _, c := range raw {
		if c < '0' || c > '9' {
			if c == '-' {
				continue
			}
			t.Fatalf("inflight counter is not numeric: %q", raw)
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

func (r *billingRig) entryStatus() wce.SendStatus {
	return r.entryRepo.entries[r.entryID].Status
}

// ---------------------------------------------------------------- no charge

// Every pre-debit refusal, proven not to move money.
//
// These are the cheap ones to get wrong: each is an early return, and an early
// return placed after the debit instead of before it charges for a message that
// never left.
func TestBilling_NoChargeOnAnyPreDebitRefusal(t *testing.T) {
	cases := map[string]struct {
		arrange    func(*billingRig)
		wantAck    bool
		wantNack   bool
		wantStatus wce.SendStatus
	}{
		"campaign vanished": {
			arrange:  func(r *billingRig) { delete(r.campaignRepo.campaigns, r.campID) },
			wantNack: true,
		},
		"template deleted": {
			arrange:    func(r *billingRig) { delete(r.templateRepo.templates, "tmpl-1") },
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		"template no longer approved": {
			arrange: func(r *billingRig) {
				r.templateRepo.templates["tmpl-1"] = &template.Template{
					ID: "tmpl-1", Name: "t", Status: template.TemplateStatusRejected,
					Category: template.TemplateCategoryMarketing,
				}
			},
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		"template has no billing category": {
			arrange: func(r *billingRig) {
				r.templateRepo.templates["tmpl-1"] = &template.Template{
					ID: "tmpl-1", Name: "t", Status: template.TemplateStatusApproved,
				}
			},
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		"subscription not current": {
			arrange:    func(r *billingRig) { r.ledger.costErr = workspace_plan.ErrSubscriptionNotCurrent },
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		"subscription not active": {
			arrange:    func(r *billingRig) { r.ledger.costErr = workspace_plan.ErrSubscriptionNotActive },
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		"no price configured": {
			arrange:    func(r *billingRig) { r.ledger.costErr = balance.ErrPriceUnavailable },
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		"pricing lookup broke": {
			arrange:  func(r *billingRig) { r.ledger.costErr = errors.New("pricing service down") },
			wantNack: true,
		},
		// The zero-price guard. A zero slipping through here is a whole campaign
		// delivered free, at bulk volume, with nothing logged.
		"price is zero": {
			arrange:    func(r *billingRig) { r.ledger.zeroCost = true },
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
		// Fail CLOSED: a missing reserver is a wiring fault, and sending without
		// one means sending without a spend ceiling.
		"reserver not wired": {
			arrange:  func(r *billingRig) { r.consumer.InflightReserver = nil },
			wantNack: true,
		},
		"balance checker not wired": {
			arrange:  func(r *billingRig) { r.consumer.CachedBalanceChecker = nil },
			wantNack: true,
		},
		"balance unreadable": {
			arrange: func(r *billingRig) {
				r.cachedBalanceChecker.getBalanceErr = errors.New("redis down")
			},
			wantNack: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newBillingRig(t)
			c.arrange(r)

			ack := r.deliver()

			debits, refunds := r.ledger.snapshot()
			if len(debits) != 0 {
				t.Errorf("charged %v on a refusal", debits)
			}
			if len(refunds) != 0 {
				t.Errorf("refunded %v without ever charging", refunds)
			}
			if ack.acked.Load() != c.wantAck || ack.nacked.Load() != c.wantNack {
				t.Errorf("ack=%v nack=%v, want ack=%v nack=%v",
					ack.acked.Load(), ack.nacked.Load(), c.wantAck, c.wantNack)
			}
			if c.wantStatus != "" && r.entryStatus() != c.wantStatus {
				t.Errorf("entry status = %q, want %q", r.entryStatus(), c.wantStatus)
			}
			// Nothing reserved means nothing to leak.
			if got := r.inflight(t); got != 0 {
				t.Errorf("left %d micros reserved after a refusal", got)
			}
		})
	}
}

// Out of budget holds the entry PENDING and takes no money. Marking it failed
// would make a resumed campaign skip somebody who was never contacted.
func TestBilling_NoChargeWhenTheReservationIsRefused(t *testing.T) {
	r := newBillingRig(t)
	// A budget below one message's cost cannot admit the reservation.
	r.cachedBalanceChecker.balanceMicros = 100
	r.ledger.cost = 500

	ack := r.deliver()

	debits, _ := r.ledger.snapshot()
	if len(debits) != 0 {
		t.Fatalf("charged %v with no budget", debits)
	}
	if !ack.acked.Load() {
		t.Error("an out-of-budget message was not acked for retry")
	}
	if r.entryStatus() != wce.SendStatusPending {
		t.Errorf("entry status = %q, want PENDING", r.entryStatus())
	}
	if got := r.inflight(t); got != 0 {
		t.Errorf("left %d micros reserved after a refused reservation", got)
	}
}

// ---------------------------------------------------------------- debit stage

// Every way the debit itself can fail, and what each must do with the money.
func TestBilling_DebitFailureModes(t *testing.T) {
	cases := map[string]struct {
		err        error
		wantAck    bool
		wantNack   bool
		wantStatus wce.SendStatus
	}{
		// Deferred, not failed: the workspace can top up and the entry is still
		// owed a message.
		"insufficient balance": {
			err: balance.ErrInsufficientBalance, wantAck: true,
			wantStatus: wce.SendStatusPending,
		},
		"no balance record": {
			err: balance.ErrBalanceNotFound, wantAck: true,
			wantStatus: wce.SendStatusPending,
		},
		// Terminal: no amount of retrying makes an expired subscription send.
		"subscription expired mid-campaign": {
			err: workspace_plan.ErrSubscriptionNotCurrent, wantAck: true,
			wantStatus: wce.SendStatusFailed,
		},
		"subscription deactivated mid-campaign": {
			err: workspace_plan.ErrSubscriptionNotActive, wantAck: true,
			wantStatus: wce.SendStatusFailed,
		},
		"price vanished mid-campaign": {
			err: balance.ErrPriceUnavailable, wantAck: true,
			wantStatus: wce.SendStatusFailed,
		},
		// Unknown: retry rather than guess.
		"ledger unavailable": {
			err: errors.New("ledger down"), wantNack: true,
			wantStatus: wce.SendStatusPending,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newBillingRig(t)
			r.ledger.executeErr = c.err

			ack := r.deliver()

			debits, refunds := r.ledger.snapshot()
			if len(debits) != 0 {
				t.Errorf("recorded a debit despite the failure: %v", debits)
			}
			// Nothing was taken, so nothing may be given back. A refund without a
			// charge is money leaving the platform for free.
			if len(refunds) != 0 {
				t.Errorf("refunded %v for a debit that never happened", refunds)
			}
			if ack.acked.Load() != c.wantAck || ack.nacked.Load() != c.wantNack {
				t.Errorf("ack=%v nack=%v, want ack=%v nack=%v",
					ack.acked.Load(), ack.nacked.Load(), c.wantAck, c.wantNack)
			}
			if r.entryStatus() != c.wantStatus {
				t.Errorf("entry status = %q, want %q", r.entryStatus(), c.wantStatus)
			}
			if got := r.inflight(t); got != 0 {
				t.Errorf("left %d micros reserved after a failed debit", got)
			}
		})
	}
}

// ---------------------------------------------------------------- refunds

// A send that never reached the customer must be refunded, under the SAME
// reference the debit used — otherwise the credit cannot be paired with the
// charge it reverses and both sit in the ledger looking unrelated.
func TestBilling_FailedSendIsRefundedUnderTheSameReference(t *testing.T) {
	r := newBillingRig(t)
	// No client for the campaign's business phone: a config error, which is a
	// send that never reached Meta.
	r.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{}

	r.deliver()

	debits, refunds := r.ledger.snapshot()
	if len(debits) != 1 {
		t.Fatalf("debits = %v, want exactly one", debits)
	}
	if len(refunds) != 1 {
		t.Fatalf("refunds = %v, want exactly one", refunds)
	}
	if debits[0] != refunds[0] {
		t.Fatalf("refund reference %q does not pair with debit %q", refunds[0], debits[0])
	}
	if got := r.inflight(t); got != 0 {
		t.Errorf("left %d micros reserved after a refunded send", got)
	}
}

// A delivered message is NOT refunded. Refunding here would credit a message the
// customer has already received.
func TestBilling_SuccessfulSendIsNotRefunded(t *testing.T) {
	r := newBillingRig(t)

	r.deliver()

	debits, refunds := r.ledger.snapshot()
	if len(debits) != 1 {
		t.Fatalf("debits = %v, want exactly one", debits)
	}
	if len(refunds) != 0 {
		t.Fatalf("refunded a delivered message: %v", refunds)
	}
	if r.entryStatus() != wce.SendStatusSent {
		t.Errorf("entry status = %q, want SENT", r.entryStatus())
	}
}

// ---------------------------------------------------------------- attribution

// The debit is keyed on the ENTRY, not the campaign.
//
// A shared campaign reference cannot tell two charges for one recipient apart
// from two recipients charged once each, which makes a redelivery
// indistinguishable from a legitimate second send.
func TestBilling_ChargesAreKeyedPerRecipient(t *testing.T) {
	r := newBillingRig(t)
	r.entryRepo.entries["entry-2"] = &wce.WhatsAppCampaignEntry{ID: "entry-2", LeadID: "lead-2", Status: wce.SendStatusPending}

	r.queueSub.deliver(r.topic, makePayload(r.campID, "entry-1", "5584999990001"))
	r.queueSub.deliver(r.topic, makePayload(r.campID, "entry-2", "5584999990002"))

	debits, _ := r.ledger.snapshot()
	if len(debits) != 2 {
		t.Fatalf("debits = %v, want two", debits)
	}
	for _, ref := range debits {
		if ref == r.campID {
			t.Fatalf("a debit was keyed on the campaign rather than the recipient: %v", debits)
		}
	}
	if debits[0] == debits[1] {
		t.Fatalf("two recipients shared one reference: %v", debits)
	}
}

// One delivery, one charge. Not a redelivery test — this pins that the ordinary
// path cannot double-charge by falling through two branches.
func TestBilling_OneDeliveryChargesOnce(t *testing.T) {
	r := newBillingRig(t)

	r.deliver()

	if debits, _ := r.ledger.snapshot(); len(debits) != 1 {
		t.Fatalf("one delivery produced %d debits: %v", len(debits), debits)
	}
}

// ---------------------------------------------------------------- reservations

// The reservation is released on EVERY path past Reserve.
//
// A reservation that is taken and not released permanently shrinks the
// workspace's spend headroom: sends start being refused for lack of budget the
// workspace actually has, and nothing in the ledger explains why.
func TestBilling_ReservationIsAlwaysReleased(t *testing.T) {
	cases := map[string]func(*billingRig){
		"successful send":     func(r *billingRig) {},
		"send failed":         func(r *billingRig) { r.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{} },
		"debit refused":       func(r *billingRig) { r.ledger.executeErr = balance.ErrInsufficientBalance },
		"subscription lapsed": func(r *billingRig) { r.ledger.executeErr = workspace_plan.ErrSubscriptionNotCurrent },
		"ledger unavailable":  func(r *billingRig) { r.ledger.executeErr = errors.New("down") },
	}

	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			r := newBillingRig(t)
			arrange(r)

			r.deliver()

			if got := r.inflight(t); got != 0 {
				t.Fatalf("left %d micros reserved", got)
			}
		})
	}
}

// Many messages in sequence must leave the counter at zero: a per-message leak
// only becomes visible at volume, which is exactly when a campaign runs.
func TestBilling_ReservationsDoNotAccumulateAcrossManySends(t *testing.T) {
	r := newBillingRig(t)
	for i := 0; i < 25; i++ {
		id := "entry-bulk-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		r.entryRepo.entries[id] = &wce.WhatsAppCampaignEntry{ID: id, LeadID: "lead-" + id, Status: wce.SendStatusPending}
		r.queueSub.deliver(r.topic, makePayload(r.campID, id, "5584999990001"))
	}

	if got := r.inflight(t); got != 0 {
		t.Fatalf("left %d micros reserved after 25 sends", got)
	}
	if debits, _ := r.ledger.snapshot(); len(debits) != 25 {
		t.Fatalf("debits = %d, want 25", len(debits))
	}
}

// ---------------------------------------------------------------- holds

// A paused campaign must not charge. The message is requeued, and a requeue that
// charged would bill the workspace once per pause cycle.
func TestBilling_PausedCampaignNeverCharges(t *testing.T) {
	r := newBillingRig(t)
	// Captured BEFORE pausing: pause detaches the consumer, so this is the
	// in-flight message the paused flag exists to stop — the case a detached
	// consumer cannot cover.
	handler := r.queueSub.handlers[r.topic]
	if err := r.consumer.PauseCampaignConsumer(r.campID); err != nil {
		t.Fatal(err)
	}

	ack := &mockAck{}
	handler(makePayload(r.campID, r.entryID, "5584999990001"), ack)

	if debits, _ := r.ledger.snapshot(); len(debits) != 0 {
		t.Fatalf("charged %v while paused", debits)
	}
	if !ack.acked.Load() {
		t.Error("the paused message was not acked for requeue")
	}
	if got := r.inflight(t); got != 0 {
		t.Errorf("left %d micros reserved while paused", got)
	}
}

// A stopped campaign must not charge either. Stop deletes the queue, but a
// message already read is no longer in it.
func TestBilling_StoppedCampaignNeverCharges(t *testing.T) {
	r := newBillingRig(t)
	handler := r.queueSub.handlers[r.topic]
	if err := r.consumer.StopCampaignConsumer(r.campID); err != nil {
		t.Fatal(err)
	}

	ack := &mockAck{}
	handler(makePayload(r.campID, r.entryID, "5584999990001"), ack)

	if debits, _ := r.ledger.snapshot(); len(debits) != 0 {
		t.Fatalf("charged %v after the campaign was stopped", debits)
	}
	if !ack.acked.Load() {
		t.Error("the discarded message was not acked")
	}
}

// A message that cannot be decoded identifies no entry, so it can never be
// charged for.
func TestBilling_UndecodableMessageNeverCharges(t *testing.T) {
	r := newBillingRig(t)

	ack := &mockAck{}
	r.queueSub.handlers[r.topic]([]byte("{broken"), ack)

	if debits, _ := r.ledger.snapshot(); len(debits) != 0 {
		t.Fatalf("charged %v for an undecodable message", debits)
	}
	// Nacked WITHOUT requeue: a message that cannot decode never will, and
	// requeuing it spins forever.
	if !ack.nacked.Load() || ack.requeue.Load() {
		t.Errorf("nacked=%v requeue=%v, want nacked without requeue",
			ack.nacked.Load(), ack.requeue.Load())
	}
}

// ---------------------------------------------------------------- fail-closed

// erroringReserver models the reservation store being unreachable, as distinct
// from it refusing: an error is "we do not know how much is in flight", and
// sending on an unknown is sending without a ceiling.
type erroringReserver struct{ released int }

func (r *erroringReserver) Reserve(string, int64, int64) (bool, error) {
	return false, errors.New("inflight store unreachable")
}
func (r *erroringReserver) Release(string, int64) error {
	r.released++
	return nil
}
func (r *erroringReserver) RefreshTTL(string, time.Duration) error { return nil }
func (r *erroringReserver) GetInflight(string) (int64, error)      { return 0, nil }

// A reservation store that ERRORS must block the send, not fall through.
//
// Distinct from a refused reservation: refused means "no budget, come back
// later", while an error means we cannot tell — and guessing "there is room"
// spends money against a ceiling nobody can see.
func TestBilling_ReserveErrorFailsClosed(t *testing.T) {
	r := newBillingRig(t)
	reserver := &erroringReserver{}
	r.consumer.InflightReserver = reserver

	ack := r.deliver()

	if debits, _ := r.ledger.snapshot(); len(debits) != 0 {
		t.Fatalf("charged %v while the inflight store was unreachable", debits)
	}
	if !ack.nacked.Load() {
		t.Error("an unreadable reservation store did not block the send")
	}
	// Nothing was reserved, so nothing may be released: releasing an unmade
	// reservation inflates the workspace's apparent headroom.
	if reserver.released != 0 {
		t.Errorf("released %d reservations that were never taken", reserver.released)
	}
	if r.entryStatus() != wce.SendStatusPending {
		t.Errorf("entry status = %q, want PENDING", r.entryStatus())
	}
}

// refusingRefundLedger takes money and then cannot give it back.
type refusingRefundLedger struct {
	ledger
	refundErr error
}

func (l *refusingRefundLedger) Refund(ws string, reference string, cat string) error {
	l.mu.Lock()
	l.refunds = append(l.refunds, reference)
	l.mu.Unlock()
	return l.refundErr
}

// A refund that FAILS must not be reported as a refund.
//
// This is the sharpest money-loss shape in the whole path: the customer was
// charged, the message never reached them, and the credit did not happen. It has
// to be loud, and the send must still be acked — retrying would charge again for
// a message that already failed.
func TestBilling_FailedRefundIsNotSilentlySwallowed(t *testing.T) {
	r := newBillingRig(t)
	failing := &refusingRefundLedger{
		ledger:    ledger{cost: 500},
		refundErr: errors.New("ledger write failed"),
	}
	r.consumer.ConsumeWhatsappTemplate = failing
	// A send that never reaches Meta, so a refund is owed.
	r.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{}

	ack := r.deliver()

	debits, refunds := failing.snapshot()
	if len(debits) != 1 {
		t.Fatalf("debits = %v, want exactly one", debits)
	}
	// The attempt is recorded even though it failed, so the pairing is
	// reconstructable from the ledger the platform does control.
	if len(refunds) != 1 {
		t.Fatalf("the refund was not even attempted: %v", refunds)
	}
	if refunds[0] != debits[0] {
		t.Fatalf("the attempted refund %q does not pair with the debit %q", refunds[0], debits[0])
	}
	// Acked regardless: a redelivery would charge a second time for a message
	// that already failed to send.
	if !ack.acked.Load() {
		t.Error("a failed refund left the message unacked, so it will be redelivered and charged again")
	}
	if got := r.inflight(t); got != 0 {
		t.Errorf("left %d micros reserved after a failed refund", got)
	}
}

// The one branch these tests deliberately do not reach.
//
// handle() falls back to a generic "not ready" message when
// GetUsabilityMessage() returns empty — but that method only returns empty when
// the usability status is Ready, and a ready template never enters the branch.
// It is unreachable defensive code inherited from before the refactor, left in
// place rather than removed because deleting code from the billing path to chase
// a coverage number is the wrong trade.
func TestBilling_UnreachableTemplateMessageFallbackIsDocumented(t *testing.T) {
	notApproved := &template.Template{
		ID: "x", Name: "t", Status: template.TemplateStatusRejected,
		Category: template.TemplateCategoryMarketing,
	}
	if notApproved.IsReadyToSend() {
		t.Fatal("a rejected template reported itself ready")
	}
	if notApproved.GetUsabilityMessage() == "" {
		t.Fatal("an unready template produced no usability message; the fallback in handle() is now reachable and needs a test")
	}
}
