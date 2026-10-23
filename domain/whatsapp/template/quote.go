package template

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"vozko/domain/shared"
)

var ErrQuoteOutOfRange = errors.New("whatsapp template quote: eligible count or cost out of range")

type SendCost struct {
	Category        string
	UnitPriceMicros int64
	Eligible        int64
	CostMicros      int64
	BalanceMicros   int64
	Currency        string
	Affordable      bool
}

func Quote(unitPriceMicros, eligible, balanceMicros int64, currency string) (SendCost, error) {
	if unitPriceMicros <= 0 {
		return SendCost{}, ErrPricingUnavailable
	}
	if eligible < 0 || (eligible > 0 && unitPriceMicros > math.MaxInt64/eligible) {
		return SendCost{}, ErrQuoteOutOfRange
	}
	if strings.TrimSpace(currency) == "" {
		return SendCost{}, fmt.Errorf("%w: quote currency is missing", ErrBillingNotConfigured)
	}
	cost := unitPriceMicros * eligible
	return SendCost{
		UnitPriceMicros: unitPriceMicros,
		Eligible:        eligible,
		CostMicros:      cost,
		BalanceMicros:   balanceMicros,
		Currency:        currency,
		Affordable:      balanceMicros >= cost,
	}, nil
}

type SendCostBRL struct {
	UnitPriceBRL float64
	CostBRL      float64
	BalanceBRL   float64
}

func (c SendCost) InBRL(rate float64) SendCostBRL {
	return SendCostBRL{
		UnitPriceBRL: shared.EstimateBRLFromMicros(c.UnitPriceMicros, rate),
		CostBRL:      shared.EstimateBRLFromMicros(c.CostMicros, rate),
		BalanceBRL:   shared.EstimateBRLFromMicros(c.BalanceMicros, rate),
	}
}
