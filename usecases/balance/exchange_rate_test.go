package balance_usecase

import (
	"errors"
	"testing"
)

func TestExchangeRateSourceReadsTheWalletRate(t *testing.T) {
	rate, err := NewExchangeRateSource(emailRateFake{micros: 5_400_000}).CurrentRate()
	if err != nil || rate != 5.4 {
		t.Fatalf("rate = %v, %v, want 5.4", rate, err)
	}
	if _, err := NewExchangeRateSource(emailRateFake{}).CurrentRate(); !errors.Is(err, ErrExchangeRateUnavailable) {
		t.Fatalf("err = %v, a missing rate is unavailable", err)
	}
	if _, err := NewExchangeRateSource(nil).CurrentRate(); !errors.Is(err, ErrExchangeRateUnavailable) {
		t.Fatalf("err = %v, a missing reader is unavailable", err)
	}
}
