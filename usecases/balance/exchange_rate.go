package balance_usecase

import (
	"errors"
	"fmt"
	"time"
)

var ErrExchangeRateUnavailable = errors.New("balance: the dollar to real exchange rate is not available")

type ExchangeRate struct {
	Rate      float64
	UpdatedAt time.Time
}

func ReadExchangeRate(reader ExchangeRateReader) (ExchangeRate, error) {
	if reader == nil {
		return ExchangeRate{}, ErrExchangeRateUnavailable
	}
	item, err := reader.Execute()
	if err != nil {
		return ExchangeRate{}, fmt.Errorf("%w: %v", ErrExchangeRateUnavailable, err)
	}
	if item == nil || item.PriceMicros <= 0 {
		return ExchangeRate{}, ErrExchangeRateUnavailable
	}
	return ExchangeRate{Rate: float64(item.PriceMicros) / 1_000_000, UpdatedAt: item.UpdatedAt.UTC()}, nil
}

func CurrentExchangeRate(reader ExchangeRateReader) (float64, error) {
	rate, err := ReadExchangeRate(reader)
	return rate.Rate, err
}
