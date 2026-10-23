package balance_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/emailsending"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

func emailPricesWithoutAttachments() emailPricerFake {
	prices := emailPrices()
	delete(prices.prices, workspace_pricing.EmailSendPerChunk)
	return prices
}

func TestAReplyWithAttachmentsIsOneChargeOfTheMessageAndItsChunks(t *testing.T) {
	ledger := newEmailLedger(10_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})
	encoded := 3*emailsending.OutboundChunkBytes + 1
	q, err := uc.ChargeMessageWithAttachments(emailCtx, "ws-1", "reply-1", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if q.Units != 1 || q.UnitMicros != 583+4*102 || q.UnitCostMicros != 165+4*29 || q.TotalMicros != 583+4*102 {
		t.Fatalf("quote = %+v", q)
	}
	if len(ledger.debits) != 1 {
		t.Fatalf("debits = %d, want one ledger entry", len(ledger.debits))
	}
	d := ledger.debits[0]
	if d.Amount != 991 || d.CostMicros != 281 || *d.ReferenceID != "email_message:reply-1" || d.ServiceType != balance.ServiceEmail {
		t.Fatalf("debit = %+v", d)
	}
	if err := uc.RefundMessage(emailCtx, "ws-1", "reply-1", q); err != nil {
		t.Fatal(err)
	}
	if len(ledger.credits) != 1 || ledger.credits[0].Amount != 991 || ledger.credits[0].CostMicros != 281 || ledger.amount != 10_000 {
		t.Fatalf("refund = %+v, amount %d", ledger.credits, ledger.amount)
	}
}

func TestAReplyWithoutAttachmentsCostsOnlyTheMessage(t *testing.T) {
	ledger := newEmailLedger(10_000)
	q, err := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{}).ChargeMessageWithAttachments(emailCtx, "ws-1", "reply-2", 0)
	if err != nil || q.TotalMicros != 583 || q.UnitCostMicros != 165 || len(ledger.debits) != 1 {
		t.Fatalf("quote = %+v, %v", q, err)
	}
}

func TestEmailPriceListCarriesTheAttachmentChunkPrice(t *testing.T) {
	got, err := NewEmailPriceList(emailPrices(), emailRateFake{micros: 6_000_000}).Execute(emailCtx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SendAttachments.PerChunk.UnitMicros != 102 || got.SendAttachments.PerChunk.Service != workspace_pricing.EmailServiceSend ||
		got.SendAttachments.PerChunk.Metric != workspace_pricing.EmailMetricPerChunk || got.SendAttachments.ChunkBytes != emailsending.OutboundChunkBytes {
		t.Fatalf("attachments = %+v", got.SendAttachments)
	}
	if _, err := NewEmailPriceList(emailPricesWithoutAttachments(), emailRateFake{micros: 6_000_000}).Execute(emailCtx, "ws-1"); !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("a missing attachment price = %v, the list fails closed", err)
	}
}

func TestAttachmentChargeRefusals(t *testing.T) {
	cases := []struct {
		name   string
		ledger *emailLedgerFake
		pricer emailPricerFake
		key    string
		want   error
	}{
		{"no chunk price", newEmailLedger(10_000), emailPricesWithoutAttachments(), "k", balance.ErrPriceUnavailable},
		{"short wallet", newEmailLedger(600), emailPrices(), "k", balance.ErrInsufficientBalance},
		{"no key", newEmailLedger(10_000), emailPrices(), " ", balance.ErrReferenceRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newConsumeEmail(tc.ledger, tc.pricer, emailSubscriptionFake{}).ChargeMessageWithAttachments(emailCtx, "ws-1", tc.key, emailsending.OutboundChunkBytes)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(tc.ledger.debits) != 0 {
				t.Fatal("a refused charge debited the wallet")
			}
		})
	}
}
