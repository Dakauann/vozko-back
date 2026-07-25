package affiliate

import "context"

type ExchangeRateProvider interface {
	CurrentRateMicros(ctx context.Context) (int64, error)
}
