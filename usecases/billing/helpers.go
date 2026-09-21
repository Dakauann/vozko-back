package billing_usecase

import (
	"time"

	billing "vozko/domain/billing"
)

type clockFn func() time.Time

func brtNow() time.Time { return time.Now().In(billing.LocationBRT()) }
