package balance_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/emailsending"
)

func TestAnAPISendIsQuotedPerRecipientWithItsAttachmentChunks(t *testing.T) {
	uc := newConsumeEmail(newEmailLedger(100_000), emailPrices(), emailSubscriptionFake{})
	q, err := uc.QuoteSend(emailCtx, "ws-1", 3, 2*emailsending.OutboundChunkBytes)
	if err != nil {
		t.Fatal(err)
	}
	if q.Units != 3 || q.UnitMicros != 583+2*102 || q.UnitCostMicros != 165+2*29 || q.TotalMicros != 3*(583+2*102) {
		t.Fatalf("quote = %+v", q)
	}
	plain, err := uc.QuoteSend(emailCtx, "ws-1", 2, 0)
	if err != nil || plain.UnitMicros != 583 || plain.TotalMicros != 1166 {
		t.Fatalf("plain quote = %+v %v", plain, err)
	}
	if _, err := uc.QuoteSend(emailCtx, "ws-1", 0, 0); !errors.Is(err, ErrEmailUnitsInvalid) {
		t.Fatalf("zero units = %v", err)
	}
}

func TestAnAPISendRoundIsOneDebitAndOneRefund(t *testing.T) {
	ledger := newEmailLedger(100_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})
	q, _ := uc.QuoteSend(emailCtx, "ws-1", 4, 0)
	recorded, err := uc.ChargeSend(emailCtx, "ws-1", "email_api:s-1#1", q)
	if err != nil || !recorded {
		t.Fatalf("charge = %v %v", recorded, err)
	}
	again, err := uc.ChargeSend(emailCtx, "ws-1", "email_api:s-1#1", q)
	if err != nil || again {
		t.Fatalf("repeat charge = %v %v, want nothing recorded", again, err)
	}
	if len(ledger.debits) != 1 || ledger.debits[0].Amount != 4*583 || *ledger.debits[0].ReferenceID != "email_api:s-1#1" {
		t.Fatalf("debits = %+v", ledger.debits)
	}
	if err := uc.RefundSend(emailCtx, "ws-1", "email_api:s-1#1", q, 3); err != nil {
		t.Fatal(err)
	}
	if err := uc.RefundSend(emailCtx, "ws-1", "email_api:s-1#1", q, 3); err != nil {
		t.Fatal(err)
	}
	if len(ledger.credits) != 1 || ledger.credits[0].Amount != 3*583 || ledger.amount != 100_000-583 {
		t.Fatalf("refunds = %+v, amount %d", ledger.credits, ledger.amount)
	}
	if err := uc.RefundSend(emailCtx, "ws-1", "email_api:s-1#2", q, 0); err != nil {
		t.Fatalf("a refund of nothing = %v", err)
	}
	if err := uc.RefundSend(emailCtx, "ws-1", "email_api:s-1#2", q, 5); !errors.Is(err, ErrEmailUnitsInvalid) {
		t.Fatalf("a refund over the round = %v", err)
	}
}

func TestAShortWalletRefusesTheWholeRoundWithWhatIsMissing(t *testing.T) {
	ledger := newEmailLedger(1_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})
	q, _ := uc.QuoteSend(emailCtx, "ws-1", 3, 0)
	_, err := uc.ChargeSend(emailCtx, "ws-1", "email_api:s-2#1", q)
	var short *InsufficientEmailBalanceError
	if !errors.As(err, &short) || !errors.Is(err, balance.ErrInsufficientBalance) || short.MissingMicros != 3*583-1_000 {
		t.Fatalf("err = %v", err)
	}
	if len(ledger.debits) != 0 {
		t.Fatal("a short wallet must not be debited")
	}
	if _, err := uc.ChargeSend(emailCtx, "ws-1", " ", q); !errors.Is(err, balance.ErrReferenceRequired) {
		t.Fatalf("no reference = %v", err)
	}
}
