package balance_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type emailRateFake struct {
	micros    int64
	updatedAt time.Time
	err       error
}

func (f emailRateFake) Execute() (*workspace_pricing.PricingItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &workspace_pricing.PricingItem{PriceMicros: f.micros, UpdatedAt: f.updatedAt}, nil
}

func TestEmailPriceListConvertsAtTheCurrentRate(t *testing.T) {
	got, err := NewEmailPriceList(emailPrices(), emailRateFake{micros: 6_000_000}).Execute(emailCtx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExchangeRate != 6 || got.Send.UnitMicros != 583 || got.Send.Metric != workspace_pricing.EmailMetricPerMessage {
		t.Fatalf("list = %+v", got)
	}
	if got.Send.UnitBRL != 0.003498 || got.Send.ThousandBRL != 3.498 {
		t.Fatalf("brl = %+v", got.Send)
	}
}

func TestEmailPriceListFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		pricer emailPricerFake
		rate   emailRateFake
		want   error
	}{
		{"no exchange rate", emailPrices(), emailRateFake{err: errors.New("db down")}, ErrExchangeRateUnavailable},
		{"zero exchange rate", emailPrices(), emailRateFake{}, ErrExchangeRateUnavailable},
		{"no send price", emailPricerFake{prices: map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult{}}, emailRateFake{micros: 6_000_000}, balance.ErrPriceUnavailable},
		{"zero send price", emailPricerFake{prices: map[workspace_pricing.EmailPriceLine]workspace_pricing.PriceResult{workspace_pricing.EmailSendPerMessage: {}}}, emailRateFake{micros: 6_000_000}, balance.ErrPriceUnavailable},
	}
	for _, tc := range cases {
		if _, err := NewEmailPriceList(tc.pricer, tc.rate).Execute(emailCtx, "ws-1"); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestEmailPriceListCarriesTheRateUpdateTime(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	got, err := NewEmailPriceList(emailPrices(), emailRateFake{micros: 6_000_000, updatedAt: at}).Execute(emailCtx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExchangeRateUpdatedAt.Equal(at) {
		t.Fatalf("updated at = %v, want %v", got.ExchangeRateUpdatedAt, at)
	}
}

func TestReadExchangeRate(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	got, err := ReadExchangeRate(emailRateFake{micros: 5_500_000, updatedAt: at})
	if err != nil || got.Rate != 5.5 || !got.UpdatedAt.Equal(at) {
		t.Fatalf("rate = %+v err = %v", got, err)
	}
	for _, bad := range []ExchangeRateReader{nil, emailRateFake{}, emailRateFake{err: errors.New("down")}} {
		if _, err := ReadExchangeRate(bad); !errors.Is(err, ErrExchangeRateUnavailable) {
			t.Errorf("err = %v", err)
		}
	}
}

func TestEmailPriceListCarriesTheReceivingLines(t *testing.T) {
	got, err := NewEmailPriceList(emailPrices(), emailRateFake{micros: 6_000_000}).Execute(emailCtx, "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Receive.PerMessage.UnitMicros != 583 || got.Receive.PerMessage.Service != workspace_pricing.EmailServiceReceive || got.Receive.PerMessage.Metric != workspace_pricing.EmailMetricPerMessage {
		t.Fatalf("per message = %+v", got.Receive.PerMessage)
	}
	if got.Receive.PerChunk.UnitMicros != 315 || got.Receive.PerChunk.Metric != workspace_pricing.EmailMetricPerChunk || got.Receive.PerChunk.UnitBRL != 0.00189 {
		t.Fatalf("per chunk = %+v", got.Receive.PerChunk)
	}
	if got.Receive.ChunkBytes != 256<<10 {
		t.Fatalf("chunk bytes = %d", got.Receive.ChunkBytes)
	}
}

func TestEmailPriceListRefusesAnUnpricedReceivingLine(t *testing.T) {
	pricer := emailPrices()
	delete(pricer.prices, workspace_pricing.EmailReceivePerChunk)
	if _, err := NewEmailPriceList(pricer, emailRateFake{micros: 6_000_000}).Execute(emailCtx, "ws-1"); !errors.Is(err, balance.ErrPriceUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
