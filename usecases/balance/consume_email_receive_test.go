package balance_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

func TestChargeReceive(t *testing.T) {
	const chunk = 256 << 10
	cases := []struct {
		name      string
		start     int64
		size      int64
		wantTotal int64
		wantCost  int64
		wantAfter int64
	}{
		{name: "a short reply pays only the message", start: 10_000, size: 20 << 10, wantTotal: 583, wantCost: 190, wantAfter: 9_417},
		{name: "complete 256 KB chunks are added", start: 10_000, size: 3*chunk + 1, wantTotal: 583 + 3*315, wantCost: 190 + 3*90, wantAfter: 10_000 - 583 - 945},
		{name: "a wallet without funds still pays, into the negative", start: 100, size: chunk, wantTotal: 898, wantCost: 280, wantAfter: 100 - 898},
		{name: "an already negative wallet goes further down", start: -500, size: 0, wantTotal: 583, wantCost: 190, wantAfter: -1_083},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := newEmailLedger(tc.start)
			uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{err: errors.New("never consulted")})
			got, err := uc.ChargeReceive(emailCtx, "ws-1", "ses-1", tc.size)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Charged || got.TotalMicros != tc.wantTotal || got.BalanceAfterMicros != tc.wantAfter {
				t.Fatalf("charge = %+v", got)
			}
			if len(ledger.debits) != 1 {
				t.Fatalf("debits = %d", len(ledger.debits))
			}
			d := ledger.debits[0]
			if !d.AllowNegative || !d.OncePerReference || *d.ReferenceID != "email_receive:ses-1" || d.ServiceType != balance.ServiceEmail ||
				d.Amount != tc.wantTotal || d.CostMicros != tc.wantCost || d.ProfitMicros != tc.wantTotal-tc.wantCost {
				t.Fatalf("debit = %+v", d)
			}
		})
	}
}

func TestChargeReceiveIsOncePerProviderMessage(t *testing.T) {
	ledger := newEmailLedger(1_000)
	uc := newConsumeEmail(ledger, emailPrices(), emailSubscriptionFake{})
	first, err := uc.ChargeReceive(emailCtx, "ws-1", "ses-1", 0)
	if err != nil || !first.Charged {
		t.Fatalf("first = %+v, %v", first, err)
	}
	again, err := uc.ChargeReceive(emailCtx, "ws-1", "ses-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if again.Charged || again.BalanceAfterMicros != 417 || len(ledger.debits) != 1 {
		t.Fatalf("replay = %+v, debits %d", again, len(ledger.debits))
	}
}

func TestChargeReceiveFailsClosed(t *testing.T) {
	noChunkPrice := emailPrices()
	delete(noChunkPrice.prices, workspace_pricing.EmailReceivePerChunk)
	readFails := newEmailLedger(0)
	readFails.readErr = errors.New("db down")
	cases := []struct {
		name   string
		ledger *emailLedgerFake
		pricer emailPricerFake
		key    string
		want   error
	}{
		{name: "no reference", ledger: newEmailLedger(0), pricer: emailPrices(), key: " ", want: balance.ErrReferenceRequired},
		{name: "unpriced message line", ledger: newEmailLedger(0), pricer: emailPricerFake{prices: map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult{}}, key: "ses-1", want: balance.ErrPriceUnavailable},
		{name: "unpriced chunk line", ledger: newEmailLedger(0), pricer: noChunkPrice, key: "ses-1", want: balance.ErrPriceUnavailable},
		{name: "no wallet", ledger: &emailLedgerFake{noBalance: true, seen: map[string]bool{}}, pricer: emailPrices(), key: "ses-1", want: balance.ErrBalanceNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newConsumeEmail(tc.ledger, tc.pricer, emailSubscriptionFake{}).ChargeReceive(emailCtx, "ws-1", tc.key, 0); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	readFails.seen["email_receive:ses-1"] = true
	if _, err := newConsumeEmail(readFails, emailPrices(), emailSubscriptionFake{}).ChargeReceive(emailCtx, "ws-1", "ses-1", 0); err == nil {
		t.Fatal("a replay whose balance cannot be read must not answer a made up balance")
	}
}
