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
		"price is zero": {
			arrange:    func(r *billingRig) { r.ledger.zeroCost = true },
			wantAck:    true,
			wantStatus: wce.SendStatusFailed,
		},
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
			if got := r.inflight(t); got != 0 {
				t.Errorf("left %d micros reserved after a refusal", got)
			}
		})
	}
}

func TestBilling_NoChargeWhenTheReservationIsRefused(t *testing.T) {
	r := newBillingRig(t)
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

func TestBilling_DebitFailureModes(t *testing.T) {
	cases := map[string]struct {
		err        error
		wantAck    bool
		wantNack   bool
		wantStatus wce.SendStatus
	}{
		"insufficient balance": {
			err: balance.ErrInsufficientBalance, wantAck: true,
			wantStatus: wce.SendStatusPending,
		},
		"no balance record": {
			err: balance.ErrBalanceNotFound, wantAck: true,
			wantStatus: wce.SendStatusPending,
		},
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

func TestBilling_FailedSendIsRefundedUnderTheSameReference(t *testing.T) {
	r := newBillingRig(t)
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

func TestBilling_OneDeliveryChargesOnce(t *testing.T) {
	r := newBillingRig(t)

	r.deliver()

	if debits, _ := r.ledger.snapshot(); len(debits) != 1 {
		t.Fatalf("one delivery produced %d debits: %v", len(debits), debits)
	}
}

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

func TestBilling_PausedCampaignNeverCharges(t *testing.T) {
	r := newBillingRig(t)
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

func TestBilling_UndecodableMessageNeverCharges(t *testing.T) {
	r := newBillingRig(t)

	ack := &mockAck{}
	r.queueSub.handlers[r.topic]([]byte("{broken"), ack)

	if debits, _ := r.ledger.snapshot(); len(debits) != 0 {
		t.Fatalf("charged %v for an undecodable message", debits)
	}
	if !ack.nacked.Load() || ack.requeue.Load() {
		t.Errorf("nacked=%v requeue=%v, want nacked without requeue",
			ack.nacked.Load(), ack.requeue.Load())
	}
}

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
	if reserver.released != 0 {
		t.Errorf("released %d reservations that were never taken", reserver.released)
	}
	if r.entryStatus() != wce.SendStatusPending {
		t.Errorf("entry status = %q, want PENDING", r.entryStatus())
	}
}

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

func TestBilling_FailedRefundIsNotSilentlySwallowed(t *testing.T) {
	r := newBillingRig(t)
	failing := &refusingRefundLedger{
		ledger:    ledger{cost: 500},
		refundErr: errors.New("ledger write failed"),
	}
	r.consumer.ConsumeWhatsappTemplate = failing
	r.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{}

	ack := r.deliver()

	debits, refunds := failing.snapshot()
	if len(debits) != 1 {
		t.Fatalf("debits = %v, want exactly one", debits)
	}
	if len(refunds) != 1 {
		t.Fatalf("the refund was not even attempted: %v", refunds)
	}
	if refunds[0] != debits[0] {
		t.Fatalf("the attempted refund %q does not pair with the debit %q", refunds[0], debits[0])
	}
	if !ack.acked.Load() {
		t.Error("a failed refund left the message unacked, so it will be redelivered and charged again")
	}
	if got := r.inflight(t); got != 0 {
		t.Errorf("left %d micros reserved after a failed refund", got)
	}
}

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
