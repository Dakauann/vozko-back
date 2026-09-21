package servicemessage

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type fakePricer struct {
	price    int64
	cost     int64
	err      error
	category string
	calls    int
}

func (f *fakePricer) PriceWhatsAppCategory(_ string, metaCategory string) (workspace_pricing.PriceResult, error) {
	f.calls++
	f.category = metaCategory
	if f.err != nil {
		return workspace_pricing.PriceResult{}, f.err
	}
	return workspace_pricing.PriceResult{
		CostMicros:   f.cost,
		PriceMicros:  f.price,
		ProfitMicros: f.price - f.cost,
	}, nil
}

type fakeLedger struct {
	existing  map[string]bool
	debits    []balance.DebitBalanceInput
	existsErr error
	debitErr  error
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{existing: map[string]bool{}}
}

func (f *fakeLedger) ExistsTransactionByReferenceID(ref string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existing[ref], nil
}

func (f *fakeLedger) DebitBalance(params balance.DebitBalanceInput) (*balance.Transaction, error) {
	if f.debitErr != nil {
		return nil, f.debitErr
	}
	f.debits = append(f.debits, params)
	if params.ReferenceID != nil {
		f.existing[*params.ReferenceID] = true
	}
	return &balance.Transaction{}, nil
}

func billedReceipt() conversation.DeliveryReceipt {
	return conversation.DeliveryReceipt{
		Status: conversation.DeliveryStatusDelivered,
		Pricing: conversation.MetaPricing{
			Category: conversation.MetaPricingCategoryService,
			Billable: true,
			Model:    "PMP",
		},
	}
}

func TestAllowSendNeverGatesAnUnpricedPlan(t *testing.T) {
	pricer := &fakePricer{price: 0}
	checker := &fakeChecker{micros: 0}
	billing := mustBilling(t, pricer, newFakeLedger(), checker)

	if err := billing.AllowSend("ws-1"); err != nil {
		t.Fatalf("AllowSend() = %v, want nil: an unpriced plan is never gated", err)
	}
	if checker.reads != 0 {
		t.Errorf("the balance was read %d times for an unpriced plan, want 0", checker.reads)
	}
}

func TestAllowSendRefusesAPricedPlanThatCannotPay(t *testing.T) {
	pricer := &fakePricer{price: 50_000}
	checker := &fakeChecker{micros: 49_999}
	billing := mustBilling(t, pricer, newFakeLedger(), checker)

	err := billing.AllowSend("ws-1")
	if !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("AllowSend() = %v, want ErrInsufficientBalance", err)
	}
}

func TestAllowSendAllowsAPricedPlanThatCanPay(t *testing.T) {
	pricer := &fakePricer{price: 50_000}
	checker := &fakeChecker{micros: 50_000}
	billing := mustBilling(t, pricer, newFakeLedger(), checker)

	if err := billing.AllowSend("ws-1"); err != nil {
		t.Fatalf("AllowSend() = %v, want nil at exactly the price", err)
	}
}

func TestAllowSendIsNotTheAIFloor(t *testing.T) {
	pricer := &fakePricer{price: 1_000}
	checker := &fakeChecker{micros: balance.MinAIFloorMicros - 1}
	billing := mustBilling(t, pricer, newFakeLedger(), checker)

	if err := billing.AllowSend("ws-1"); err != nil {
		t.Fatalf("AllowSend() = %v; a workspace below the AI floor can still afford a cheap message", err)
	}
}

func TestAllowSendRefusesWhenPricingIsDown(t *testing.T) {
	pricer := &fakePricer{err: errors.New("database is down")}
	billing := mustBilling(t, pricer, newFakeLedger(), &fakeChecker{micros: 1_000_000})

	if err := billing.AllowSend("ws-1"); err == nil {
		t.Fatal("AllowSend() = nil with pricing down, want a refusal")
	}
}

func TestAllowSendPricesTheServiceCategory(t *testing.T) {
	pricer := &fakePricer{price: 10}
	billing := mustBilling(t, pricer, newFakeLedger(), &fakeChecker{micros: 1_000_000})

	_ = billing.AllowSend("ws-1")
	if pricer.category != conversation.MetaPricingCategoryService {
		t.Errorf("priced category %q, want %q", pricer.category, conversation.MetaPricingCategoryService)
	}
}

func TestChargeDeliveredBooksWhatMetaBilled(t *testing.T) {
	pricer := &fakePricer{price: 50_000, cost: 30_000}
	ledger := newFakeLedger()
	billing := mustBilling(t, pricer, ledger, &fakeChecker{micros: 1_000_000})

	if err := billing.ChargeDelivered("ws-1", billedReceipt(), "wamid.ABC"); err != nil {
		t.Fatalf("ChargeDelivered() = %v", err)
	}
	if len(ledger.debits) != 1 {
		t.Fatalf("got %d debits, want 1", len(ledger.debits))
	}

	debit := ledger.debits[0]
	if debit.ServiceType != balance.ServiceWhatsAppConversation {
		t.Errorf("service type = %q, want %q; this is the type the reserved name was waiting for",
			debit.ServiceType, balance.ServiceWhatsAppConversation)
	}
	if debit.Amount != 50_000 || debit.CostMicros != 30_000 || debit.ProfitMicros != 20_000 {
		t.Errorf("debit = amount %d cost %d profit %d, want 50000/30000/20000",
			debit.Amount, debit.CostMicros, debit.ProfitMicros)
	}
	if !debit.AllowNegative {
		t.Error("the debit did not allow a negative balance; the cost is already incurred")
	}
	if debit.ReferenceID == nil || *debit.ReferenceID != "wa-service:wamid.ABC" {
		t.Errorf("reference = %v, want the prefixed provider message id", debit.ReferenceID)
	}
}

func TestChargeDeliveredIsIdempotentOnTheProviderMessageID(t *testing.T) {
	pricer := &fakePricer{price: 50_000}
	ledger := newFakeLedger()
	billing := mustBilling(t, pricer, ledger, &fakeChecker{micros: 1_000_000})

	for _, status := range []conversation.DeliveryStatus{
		conversation.DeliveryStatusSent,
		conversation.DeliveryStatusDelivered,
		conversation.DeliveryStatusRead,
	} {
		receipt := billedReceipt()
		receipt.Status = status
		if err := billing.ChargeDelivered("ws-1", receipt, "wamid.ABC"); err != nil {
			t.Fatalf("ChargeDelivered(%s) = %v", status, err)
		}
	}

	if len(ledger.debits) != 1 {
		t.Fatalf("got %d debits for one message, want 1", len(ledger.debits))
	}
}

func TestChargeDeliveredRefusesWhatMetaDidNotBill(t *testing.T) {
	cases := map[string]func(*conversation.DeliveryReceipt){
		"Meta says not billable": func(r *conversation.DeliveryReceipt) {
			r.Pricing.Billable = false
		},
		"a template category, charged as a campaign send": func(r *conversation.DeliveryReceipt) {
			r.Pricing.Category = "marketing"
		},
		"a free customer service message": func(r *conversation.DeliveryReceipt) {
			r.Pricing.Billable = false
			r.Pricing.Category = "utility"
		},
		"Meta has said nothing about pricing": func(r *conversation.DeliveryReceipt) {
			r.Pricing = conversation.MetaPricing{}
		},
		"the send failed, and Meta charges on delivery": func(r *conversation.DeliveryReceipt) {
			r.Status = conversation.DeliveryStatusFailed
		},
		"no delivery status at all": func(r *conversation.DeliveryReceipt) {
			r.Status = conversation.DeliveryStatusNone
		},
		"inside the 72 hour free entry point": func(r *conversation.DeliveryReceipt) {
			r.ConversationOrigin = conversation.MetaOriginFreeEntryPoint
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			pricer := &fakePricer{price: 50_000}
			ledger := newFakeLedger()
			billing := mustBilling(t, pricer, ledger, &fakeChecker{micros: 1_000_000})

			receipt := billedReceipt()
			mutate(&receipt)

			if err := billing.ChargeDelivered("ws-1", receipt, "wamid.ABC"); err != nil {
				t.Fatalf("ChargeDelivered() = %v", err)
			}
			if len(ledger.debits) != 0 {
				t.Errorf("charged %d times for a message Meta did not bill", len(ledger.debits))
			}
		})
	}
}

func TestChargeDeliveredDoesNotChargeAnUnpricedPlan(t *testing.T) {
	ledger := newFakeLedger()
	billing := mustBilling(t, &fakePricer{price: 0}, ledger, &fakeChecker{micros: 0})

	if err := billing.ChargeDelivered("ws-1", billedReceipt(), "wamid.ABC"); err != nil {
		t.Fatalf("ChargeDelivered() = %v", err)
	}
	if len(ledger.debits) != 0 {
		t.Error("an unpriced plan was charged")
	}
}

func TestChargeDeliveredNeedsBothIdentifiers(t *testing.T) {
	for name, args := range map[string][2]string{
		"no workspace":    {"", "wamid.ABC"},
		"no message id":   {"ws-1", ""},
		"neither":         {"", ""},
		"whitespace only": {"   ", "   "},
	} {
		t.Run(name, func(t *testing.T) {
			ledger := newFakeLedger()
			billing := mustBilling(t, &fakePricer{price: 50_000}, ledger, &fakeChecker{micros: 1_000_000})

			if err := billing.ChargeDelivered(args[0], billedReceipt(), args[1]); err != nil {
				t.Fatalf("ChargeDelivered() = %v", err)
			}
			if len(ledger.debits) != 0 {
				t.Error("charged without both identifiers")
			}
		})
	}
}

func TestChargeDeliveredReportsAFailedIdempotencyRead(t *testing.T) {
	ledger := newFakeLedger()
	ledger.existsErr = errors.New("connection reset")
	billing := mustBilling(t, &fakePricer{price: 50_000}, ledger, &fakeChecker{micros: 1_000_000})

	if err := billing.ChargeDelivered("ws-1", billedReceipt(), "wamid.ABC"); err == nil {
		t.Fatal("ChargeDelivered() = nil when the idempotency read failed")
	}
	if len(ledger.debits) != 0 {
		t.Error("charged despite not knowing whether it had already charged")
	}
}

func TestServiceMessageReferenceIsPrefixed(t *testing.T) {
	ref := Reference("wamid.ABC")
	if ref == "wamid.ABC" {
		t.Fatal("the reference is the bare message id and could collide")
	}
	for _, other := range []string{"refund:", "waba:", "refund:waba:"} {
		if len(ref) >= len(other) && ref[:len(other)] == other {
			t.Errorf("the reference collides with the %q namespace", other)
		}
	}
}

type fakeChecker struct {
	micros int64
	err    error
	reads  int
}

func (f *fakeChecker) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (f *fakeChecker) GetBalance(string) (int64, error) {
	f.reads++
	return f.micros, f.err
}
func (f *fakeChecker) Invalidate(string)          {}
func (f *fakeChecker) InvalidateDebounced(string) {}

func mustBilling(t *testing.T, pricer Pricer, ledger Ledger, checker balance.CachedBalanceChecker) Billing {
	t.Helper()
	b, err := NewBilling(Deps{Pricer: pricer, Ledger: ledger, BalanceChecker: checker})
	if err != nil {
		t.Fatalf("NewBilling() = %v", err)
	}
	return b
}

func TestNewBillingRefusesToBuildWithoutWhatItNeeds(t *testing.T) {
	full := Deps{Pricer: &fakePricer{}, Ledger: newFakeLedger(), BalanceChecker: &fakeChecker{}}
	if _, err := NewBilling(full); err != nil {
		t.Fatalf("a fully wired billing failed to build: %v", err)
	}

	for name, break_ := range map[string]func(*Deps){
		"no pricer":          func(d *Deps) { d.Pricer = nil },
		"no ledger":          func(d *Deps) { d.Ledger = nil },
		"no balance checker": func(d *Deps) { d.BalanceChecker = nil },
	} {
		t.Run(name, func(t *testing.T) {
			deps := full
			break_(&deps)
			if _, err := NewBilling(deps); !errors.Is(err, ErrBillingNotConfigured) {
				t.Errorf("NewBilling() = %v, want ErrBillingNotConfigured", err)
			}
		})
	}
}

func TestShouldChargeAgreesWithChargeDelivered(t *testing.T) {
	cases := map[string]struct {
		mutate func(*conversation.DeliveryReceipt)
		want   bool
	}{
		"a billed service message":    {func(*conversation.DeliveryReceipt) {}, true},
		"Meta says not billable":      {func(r *conversation.DeliveryReceipt) { r.Pricing.Billable = false }, false},
		"a template category":         {func(r *conversation.DeliveryReceipt) { r.Pricing.Category = "marketing" }, false},
		"the send failed":             {func(r *conversation.DeliveryReceipt) { r.Status = conversation.DeliveryStatusFailed }, false},
		"no pricing from Meta at all": {func(r *conversation.DeliveryReceipt) { r.Pricing = conversation.MetaPricing{} }, false},
		"inside the free entry point": {func(r *conversation.DeliveryReceipt) { r.ConversationOrigin = conversation.MetaOriginFreeEntryPoint }, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ledger := newFakeLedger()
			billing := mustBilling(t, &fakePricer{price: 50_000}, ledger, &fakeChecker{micros: 1_000_000})

			receipt := billedReceipt()
			tc.mutate(&receipt)

			if got := billing.ShouldCharge(receipt); got != tc.want {
				t.Fatalf("ShouldCharge() = %v, want %v", got, tc.want)
			}

			if err := billing.ChargeDelivered("ws-1", receipt, "wamid.ABC"); err != nil {
				t.Fatalf("ChargeDelivered() = %v", err)
			}
			charged := len(ledger.debits) == 1
			if charged != tc.want {
				t.Errorf("ChargeDelivered charged = %v but ShouldCharge said %v", charged, tc.want)
			}
		})
	}
}
