package carriermessaging_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/carriermessaging"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type fakePricer struct {
	rcs map[string]workspace_pricing.PriceResult
	sms map[string]workspace_pricing.PriceResult
}

func lookup(prices map[string]workspace_pricing.PriceResult, service string) (workspace_pricing.PriceResult, error) {
	price, ok := prices[service]
	if !ok {
		return workspace_pricing.PriceResult{}, workspace_pricing.ErrPricingItemNotFound
	}
	return price, nil
}

func (f *fakePricer) PriceRCS(_ string, service string) (workspace_pricing.PriceResult, error) {
	return lookup(f.rcs, service)
}

func (f *fakePricer) PriceSMS(_ string, service string) (workspace_pricing.PriceResult, error) {
	return lookup(f.sms, service)
}

func defaultPricer() *fakePricer {
	return &fakePricer{
		rcs: map[string]workspace_pricing.PriceResult{
			"basic":        {CostMicros: 19_000, PriceMicros: 31_667, ProfitMicros: 12_667},
			"single":       {CostMicros: 38_000, PriceMicros: 63_333, ProfitMicros: 25_333},
			"conversation": {CostMicros: 66_000, PriceMicros: 126_667, ProfitMicros: 60_667},
			"inbound":      {CostMicros: 19_000, PriceMicros: 31_667, ProfitMicros: 12_667},
		},
		sms: map[string]workspace_pricing.PriceResult{
			"segment": {CostMicros: 20_000, PriceMicros: 35_000, ProfitMicros: 15_000},
			"inbound": {CostMicros: 5_000, PriceMicros: 8_333, ProfitMicros: 3_333},
		},
	}
}

type fakeLedger struct {
	debits   []balance.DebitBalanceInput
	credits  []balance.CreditBalanceInput
	seen     map[string]bool
	debitErr error
}

func newFakeLedger() *fakeLedger { return &fakeLedger{seen: map[string]bool{}} }

func (l *fakeLedger) DebitBalance(in balance.DebitBalanceInput) (*balance.Transaction, error) {
	if l.debitErr != nil {
		return nil, l.debitErr
	}
	if l.seen[*in.ReferenceID] {
		return nil, balance.ErrReferenceAlreadyRecorded
	}
	l.seen[*in.ReferenceID] = true
	l.debits = append(l.debits, in)
	return &balance.Transaction{}, nil
}

func (l *fakeLedger) CreditBalance(in balance.CreditBalanceInput) (*balance.Transaction, error) {
	if l.seen[*in.ReferenceID] {
		return nil, balance.ErrReferenceAlreadyRecorded
	}
	l.seen[*in.ReferenceID] = true
	l.credits = append(l.credits, in)
	return &balance.Transaction{}, nil
}

func TestChargeDebitsThePricingItemOncePerReference(t *testing.T) {
	ledger := newFakeLedger()
	c := NewCharges(defaultPricer(), ledger)
	for range 2 {
		if err := c.Debit("ws-1", carriermessaging.ChargeRCSSingle, "msg-1", 1, false); err != nil {
			t.Fatalf("Debit = %v", err)
		}
	}
	if len(ledger.debits) != 1 {
		t.Fatalf("debits = %d, want 1", len(ledger.debits))
	}
	d := ledger.debits[0]
	if d.Amount != 63_333 || d.CostMicros != 38_000 || *d.ReferenceID != "carrier:rcs_single:msg-1" || d.ServiceType != "rcs" || !d.OncePerReference {
		t.Fatalf("debit = %+v", d)
	}
}

func TestSMSIsChargedPerSegment(t *testing.T) {
	ledger := newFakeLedger()
	c := NewCharges(defaultPricer(), ledger)
	if err := c.Debit("ws-1", carriermessaging.ChargeSMSSegment, "sms-1", 3, false); err != nil {
		t.Fatalf("Debit = %v", err)
	}
	d := ledger.debits[0]
	if d.Amount != 105_000 || d.CostMicros != 60_000 || d.ProfitMicros != 45_000 || d.ServiceType != "sms" {
		t.Fatalf("debit = %+v", d)
	}
	if err := c.Refund("ws-1", carriermessaging.ChargeSMSSegment, "sms-1", 3); err != nil || ledger.credits[0].Amount != 105_000 {
		t.Fatalf("refund = %+v, %v", ledger.credits, err)
	}
}

func TestRefundCreditsBackOnce(t *testing.T) {
	ledger := newFakeLedger()
	c := NewCharges(defaultPricer(), ledger)
	_ = c.Debit("ws-1", carriermessaging.ChargeRCSBasic, "msg-1", 1, false)
	for range 2 {
		if err := c.Refund("ws-1", carriermessaging.ChargeRCSBasic, "msg-1", 1); err != nil {
			t.Fatalf("Refund = %v", err)
		}
	}
	if len(ledger.credits) != 1 || ledger.credits[0].Amount != 31_667 || !ledger.credits[0].IsRefund {
		t.Fatalf("credits = %+v", ledger.credits)
	}
}

func TestInboundChargesMayGoNegative(t *testing.T) {
	ledger := newFakeLedger()
	c := NewCharges(defaultPricer(), ledger)
	if err := c.Debit("ws-1", carriermessaging.ChargeRCSInbound, "in-1", 1, true); err != nil || !ledger.debits[0].AllowNegative {
		t.Fatalf("Debit = %v", err)
	}
}

func TestChargesRefuseWhatHasNoPriceOrQuantity(t *testing.T) {
	c := NewCharges(&fakePricer{}, newFakeLedger())
	if err := c.Debit("ws-1", carriermessaging.ChargeSMSSegment, "m", 1, false); !errors.Is(err, carriermessaging.ErrPriceUnavailable) {
		t.Fatalf("Debit = %v, want price unavailable", err)
	}
	c = NewCharges(defaultPricer(), newFakeLedger())
	if err := c.Debit("ws-1", carriermessaging.ChargeSMSSegment, "m", 0, false); !errors.Is(err, ErrQuantityInvalid) {
		t.Fatalf("Debit zero = %v", err)
	}
	if err := c.Debit("ws-1", "fax_page", "m", 1, false); !errors.Is(err, carriermessaging.ErrPriceUnavailable) {
		t.Fatalf("Debit unknown = %v", err)
	}
}

func TestInsufficientBalanceIsReported(t *testing.T) {
	ledger := newFakeLedger()
	ledger.debitErr = balance.ErrInsufficientBalance
	c := NewCharges(defaultPricer(), ledger)
	if err := c.Debit("ws-1", carriermessaging.ChargeRCSBasic, "m", 1, false); !errors.Is(err, balance.ErrInsufficientBalance) {
		t.Fatalf("Debit = %v", err)
	}
}
