package balance_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type referenceLedgerStub struct {
	existing map[string]bool
	debits   []balance.DebitBalanceInput
	credits  []balance.CreditBalanceInput
	lookErr  error
}

func (l *referenceLedgerStub) ExistsTransactionByReferenceID(ref string) (bool, error) {
	return l.existing[ref], l.lookErr
}

func (l *referenceLedgerStub) recordOnce(once bool, ref *string) error {
	if !once {
		return nil
	}
	if l.lookErr != nil {
		return l.lookErr
	}
	if l.existing[*ref] {
		return balance.ErrReferenceAlreadyRecorded
	}
	return nil
}

func (l *referenceLedgerStub) DebitBalance(in balance.DebitBalanceInput) (*balance.Transaction, error) {
	if err := l.recordOnce(in.OncePerReference, in.ReferenceID); err != nil {
		return nil, err
	}
	l.debits = append(l.debits, in)
	return &balance.Transaction{}, nil
}

func (l *referenceLedgerStub) CreditBalance(in balance.CreditBalanceInput) (*balance.Transaction, error) {
	if err := l.recordOnce(in.OncePerReference, in.ReferenceID); err != nil {
		return nil, err
	}
	l.credits = append(l.credits, in)
	return &balance.Transaction{}, nil
}

func sampleCharge() ReferenceCharge {
	return ReferenceCharge{
		WorkspaceID: "ws-1", ReferenceID: "ref-1", ServiceType: balance.ServiceAdvertising,
		Price:       workspace_pricing.PriceResult{CostMicros: 100, PriceMicros: 1_000, ProfitMicros: 900},
		Description: "fee",
	}
}

func TestDebitOnceChargesTheReferenceOnlyOnce(t *testing.T) {
	ledger := &referenceLedgerStub{existing: map[string]bool{}}
	tx, err := DebitOnce(ledger, sampleCharge())
	if err != nil || tx == nil || len(ledger.debits) != 1 {
		t.Fatalf("first debit tx=%v err=%v debits=%d", tx, err, len(ledger.debits))
	}
	d := ledger.debits[0]
	if d.Amount != 1_000 || d.CostMicros != 100 || d.ProfitMicros != 900 || *d.ReferenceID != "ref-1" || d.ServiceType != balance.ServiceAdvertising {
		t.Fatalf("debit %+v", d)
	}
	ledger.existing["ref-1"] = true
	tx, err = DebitOnce(ledger, sampleCharge())
	if err != nil || tx != nil || len(ledger.debits) != 1 {
		t.Fatalf("second debit tx=%v err=%v debits=%d", tx, err, len(ledger.debits))
	}
}

func TestDebitOnceRefusesWhenItCannotTellIfAlreadyCharged(t *testing.T) {
	ledger := &referenceLedgerStub{lookErr: errors.New("db down")}
	if _, err := DebitOnce(ledger, sampleCharge()); err == nil || len(ledger.debits) != 0 {
		t.Fatalf("charged blind: err=%v debits=%d", err, len(ledger.debits))
	}
}

func TestDebitOnceRefusesAnUnpricedCharge(t *testing.T) {
	c := sampleCharge()
	c.Price.PriceMicros = 0
	if _, err := DebitOnce(&referenceLedgerStub{}, c); !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("zero price err = %v", err)
	}
}

func TestRefundOnceReversesTheChargeUnderItsOwnReference(t *testing.T) {
	ledger := &referenceLedgerStub{existing: map[string]bool{}}
	if err := RefundOnce(ledger, sampleCharge()); err != nil {
		t.Fatal(err)
	}
	c := ledger.credits[0]
	if *c.ReferenceID != "refund:ref-1" || c.Amount != 1_000 || c.ProfitMicros != -900 || !c.IsRefund {
		t.Fatalf("credit %+v", c)
	}
	ledger.existing["refund:ref-1"] = true
	if err := RefundOnce(ledger, sampleCharge()); err != nil || len(ledger.credits) != 1 {
		t.Fatalf("refund paid twice: err=%v credits=%d", err, len(ledger.credits))
	}
}
