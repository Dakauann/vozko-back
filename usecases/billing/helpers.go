package billing_usecase

import (
	"time"

	billing "vozko/domain/billing"
)

// clockFn matches the existing time-injection convention. Billing logic reads "now" through it and
// never calls time.Now directly, so date-driven behavior is deterministic under test.
type clockFn func() time.Time

// brtNow reports the current instant in the billing timezone (America/Sao_Paulo), where anchor and
// cutoff day boundaries are evaluated.
func brtNow() time.Time { return time.Now().In(billing.LocationBRT()) }
