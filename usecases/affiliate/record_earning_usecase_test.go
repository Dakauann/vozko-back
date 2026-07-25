package affiliate_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/affiliate"
)

const defaultRate int64 = 6_000_000

func newRate(micros int64) *mockExchangeRateProvider {
	return &mockExchangeRateProvider{rateMicros: micros}
}

func TestRecordEarning_Success(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].CommissionPct = 0.05
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}

	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))

	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1",
		AmountUSDMicros:    33_333_333,
		ExchangeRateMicros: defaultRate,
		Purpose:            "top_up",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if e == nil || e.AmountMicros != 1_666_667 {
		t.Fatalf("unexpected micros: %+v", e)
	}
	if e.ExchangeRateMicros != defaultRate {
		t.Fatalf("expected rate frozen on row, got %d", e.ExchangeRateMicros)
	}
	if e.Purpose != "TOP_UP" {
		t.Fatalf("expected upper purpose, got %q", e.Purpose)
	}
}

func TestRecordEarning_Idempotent(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	repo.earnings["inv-1"] = &affiliate.Earning{
		ID: "existing", AffiliateID: "aff-1", InvoiceID: "inv-1",
		AmountMicros: 999, ExchangeRateMicros: defaultRate,
	}

	rate := newRate(defaultRate)
	uc := NewRecordEarningUseCase(repo, rate)
	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 33_333_333, ExchangeRateMicros: defaultRate, Purpose: "top_up",
	})
	if err != nil || e == nil || e.ID != "existing" {
		t.Fatalf("expected existing earning, got %v %+v", err, e)
	}

	if rate.calls != 0 {
		t.Fatalf("exchange-rate provider should not be called on idempotent replay")
	}
}

func TestRecordEarning_NoReferral(t *testing.T) {
	uc := NewRecordEarningUseCase(newMockRepo(), newRate(defaultRate))
	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	})
	if err != nil || e != nil {
		t.Fatalf("want nil, nil; got %v %v", e, err)
	}
}

func TestRecordEarning_InactiveAffiliate(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].Active = false
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	})
	if err != nil || e != nil {
		t.Fatalf("want nil, nil; got %v %v", e, err)
	}
}

func TestRecordEarning_InvalidInputs(t *testing.T) {
	uc := NewRecordEarningUseCase(newMockRepo(), newRate(defaultRate))
	cases := []affiliate.RecordEarningInput{
		{WorkspaceID: "", InvoiceID: "inv", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate},
		{WorkspaceID: "ws", InvoiceID: "", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate},
		{WorkspaceID: "ws", InvoiceID: "inv", AmountUSDMicros: 0, ExchangeRateMicros: defaultRate},
		{WorkspaceID: "ws", InvoiceID: "inv", AmountUSDMicros: -1, ExchangeRateMicros: defaultRate},
	}
	for _, in := range cases {
		e, err := uc.Execute(context.Background(), in)
		if err != nil || e != nil {
			t.Fatalf("want nil, nil for %+v; got %v %v", in, e, err)
		}
	}
}

func TestRecordEarning_ZeroCommission(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].CommissionPct = 0
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	})
	if err != nil || e != nil {
		t.Fatalf("want nil, nil (commission=0); got %v %v", e, err)
	}
}

func TestRecordEarning_ReferralRepoError(t *testing.T) {
	repo := newMockRepo()
	repo.failGetRef = errors.New("db")
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	if _, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws", InvoiceID: "inv", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	}); err == nil {
		t.Fatal("want error")
	}
}

func TestRecordEarning_AffiliateNotFound_Silenced(t *testing.T) {
	repo := newMockRepo()
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "missing", WorkspaceID: "ws-1"}
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	})
	if err != nil || e != nil {
		t.Fatalf("want silent nil, got %v %v", e, err)
	}
}

func TestRecordEarning_AffiliateLookupError(t *testing.T) {
	repo := newMockRepo()
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	repo.failGetByID = errors.New("db")
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	if _, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	}); err == nil {
		t.Fatal("want error")
	}
}

func TestRecordEarning_GetEarningError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	repo.failGetEarn = errors.New("db")
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	if _, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667, ExchangeRateMicros: defaultRate,
	}); err == nil {
		t.Fatal("want error")
	}
}

func TestRecordEarning_CreateError(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	repo.failCreateEarn = errors.New("db")
	uc := NewRecordEarningUseCase(repo, newRate(defaultRate))
	if _, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 33_333_333, ExchangeRateMicros: defaultRate,
	}); err == nil {
		t.Fatal("want error")
	}
}

func TestRecordEarning_NilRateProvider_FailsClosed(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewRecordEarningUseCase(repo, nil)

	_, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667,
	})
	if !errors.Is(err, affiliate.ErrExchangeRateUnavailable) {
		t.Fatalf("want ErrExchangeRateUnavailable, got %v", err)
	}
	if len(repo.earnings) != 0 {
		t.Fatalf("no earning should be persisted without a rate")
	}
}

func TestRecordEarning_RateProviderError_Propagates(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	rate := &mockExchangeRateProvider{err: errors.New("pricing down")}
	uc := NewRecordEarningUseCase(repo, rate)

	_, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667,
	})
	if err == nil || err.Error() != "pricing down" {
		t.Fatalf("want propagated provider error, got %v", err)
	}
}

func TestRecordEarning_ZeroRate_FailsClosed(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewRecordEarningUseCase(repo, newRate(0))

	_, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667,
	})
	if !errors.Is(err, affiliate.ErrExchangeRateUnavailable) {
		t.Fatalf("want ErrExchangeRateUnavailable, got %v", err)
	}
}

func TestRecordEarning_NegativeRate_FailsClosed(t *testing.T) {
	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	uc := NewRecordEarningUseCase(repo, newRate(-1))
	_, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 16_666_667,
	})
	if !errors.Is(err, affiliate.ErrExchangeRateUnavailable) {
		t.Fatalf("want ErrExchangeRateUnavailable, got %v", err)
	}
}

func TestRecordEarning_RateIsFrozen(t *testing.T) {

	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].CommissionPct = 0.05
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	rate := newRate(5_000_000)
	uc := NewRecordEarningUseCase(repo, rate)

	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1", AmountUSDMicros: 20_000_000,
	})
	if err != nil || e == nil {
		t.Fatalf("setup: %v", err)
	}
	rate.rateMicros = 7_000_000
	if e.ExchangeRateMicros != 5_000_000 {
		t.Fatalf("rate should be frozen at record time, got %d", e.ExchangeRateMicros)
	}
}

func TestRecordEarning_CallerSuppliedRate_BypassesProvider(t *testing.T) {

	repo := newMockRepo()
	seedAffiliate(repo, "aff-1", "user-1")
	repo.affiliates["aff-1"].CommissionPct = 0.10
	repo.referrals["ws-1"] = &affiliate.Referral{AffiliateID: "aff-1", WorkspaceID: "ws-1"}
	rate := newRate(6_000_000)
	uc := NewRecordEarningUseCase(repo, rate)
	e, err := uc.Execute(context.Background(), affiliate.RecordEarningInput{
		WorkspaceID: "ws-1", InvoiceID: "inv-1",
		AmountUSDMicros:    10_000_000,
		ExchangeRateMicros: 5_500_000,
	})
	if err != nil || e == nil {
		t.Fatalf("setup: %v", err)
	}
	if rate.calls != 0 {
		t.Fatalf("provider must not be called when caller supplies a positive rate (got %d calls)", rate.calls)
	}
	if e.ExchangeRateMicros != 5_500_000 {
		t.Fatalf("expected invoice rate 5_500_000 frozen on row, got %d", e.ExchangeRateMicros)
	}
	if e.AmountMicros != 1_000_000 {
		t.Fatalf("expected commission 1_000_000, got %d", e.AmountMicros)
	}
}
