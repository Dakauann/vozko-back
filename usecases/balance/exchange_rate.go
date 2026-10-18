package balance_usecase

import (
	"errors"
	"fmt"
)

var ErrExchangeRateUnavailable = errors.New("balance: the dollar to real exchange rate is not available")

func currentExchangeRate(reader ExchangeRateReader) (float64, error) {
	if reader == nil {
		return 0, ErrExchangeRateUnavailable
	}
	item, err := reader.Execute()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrExchangeRateUnavailable, err)
	}
	if item == nil || item.PriceMicros <= 0 {
		return 0, ErrExchangeRateUnavailable
	}
	return float64(item.PriceMicros) / 1_000_000, nil
}
