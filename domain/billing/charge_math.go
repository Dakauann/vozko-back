package billing

import (
	"math"
	"time"
)

// MonthlyChargeBRL composes the unified monthly charge in BRL and the saldo-creditable (plan)
// portion, also in BRL.
//
// Currency model (see the money-currency-model note): the ledger/saldo, products, and addons are all
// USD; only the plan price is BRL-native. Asaas charges in BRL. So the plan price (BRL cents) is used
// directly, and each addon's USD-micros total is converted to BRL at fxUSDToBRL. Only the plan
// becomes saldo on payment; the channel-license addons are a vendor-cost repasse and are excluded
// from the creditable portion.
//
// planBRLCents is the plan price in BRL cents for the subscription's billing cycle (the caller
// resolves it via BillingCycle.TotalPriceBRLCents, so an annual plan passes the full-year price, not
// one month). addonUSDMicros holds each active addon's already quantity-multiplied total in USD
// micros. Both returned amounts are rounded to BRL cents.
func MonthlyChargeBRL(planBRLCents int64, addonUSDMicros []int64, fxUSDToBRL float64) (totalBRL, creditableBRL float64) {
	planBRL := float64(planBRLCents) / 100.0
	var addonsBRL float64
	for _, usd := range addonUSDMicros {
		addonsBRL += float64(usd) / 1_000_000.0 * fxUSDToBRL
	}
	return roundCents(planBRL + addonsBRL), roundCents(planBRL)
}

// roundCents rounds a BRL amount to two decimal places (cents), the granularity Asaas charges at.
func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

// ActivationPeriod returns the up-front charge and the co-term period end for activating an addon whose
// quantity-multiplied price (or cost) is fullMicros, at time `at`. A NEW monthly addon is charged a
// daily proration for the partial period [at, FirstBillingAnchor] and co-terms to that anchor; annual
// addons and top-ups of an existing addon charge the full period. The charge is CAPPED at one full
// month, so a customer never pays more than a month up front.
//
// Setting the period end to a real anchor (not a calendar month-end) is what keeps the model gap-free:
// the recurring emit bills the channel on that anchor and confirm's Extend continues from it with no
// coverage gap and no double-charge. It is pure and is the SINGLE source of truth shared by the purchase
// use case and its preview, so the amount a customer is shown before buying always equals what is charged.
func ActivationPeriod(at time.Time, emitDay, dueDay, cycleMonths int, isNew bool, fullMicros int64) (charge int64, periodEnd time.Time) {
	if !isNew || cycleMonths != 1 {
		return fullMicros, at.AddDate(0, cycleMonths, 0)
	}
	atBRT := at.In(LocationBRT())
	first := FirstBillingAnchor(atBRT, emitDay, dueDay)
	daysInCycle := daysBetweenDates(first.AddDate(0, -1, 0), first)
	daysUpFront := daysBetweenDates(atBRT, first)
	if daysInCycle <= 0 {
		return fullMicros, first
	}
	c := int64(math.Round(float64(fullMicros) * float64(daysUpFront) / float64(daysInCycle)))
	if c > fullMicros { // never charge more than one full month up front
		c = fullMicros
	}
	if c < 0 {
		c = 0
	}
	return c, first
}

// FirstBillingAnchor is the first anchor whose unified invoice will include a channel activated at `at`.
// A channel active before this month's emit day is on this month's invoice (this month's anchor); one
// activated on or after the emit day missed that emit, so its first invoice is next month's anchor. This
// is the boundary that makes the proration gap-free.
func FirstBillingAnchor(at time.Time, emitDay, dueDay int) time.Time {
	atBRT := at.In(LocationBRT())
	if atBRT.Day() < emitDay {
		return anchorDate(atBRT.Year(), atBRT.Month(), dueDay, atBRT.Location())
	}
	y, m := addMonth(atBRT.Year(), atBRT.Month())
	return anchorDate(y, m, dueDay, atBRT.Location())
}

// ActivationProRataDays is the number of days the up-front activation charge covers (from `at` to the
// first billing anchor). It exists so the purchase preview can explain the proration to the customer.
func ActivationProRataDays(at time.Time, emitDay, dueDay int) int {
	atBRT := at.In(LocationBRT())
	return daysBetweenDates(atBRT, FirstBillingAnchor(atBRT, emitDay, dueDay))
}

// daysBetweenDates counts whole calendar days from a's date to b's date (both taken at midnight in a's
// location), so time-of-day never shifts the count.
func daysBetweenDates(a, b time.Time) int {
	ad := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, a.Location())
	bd := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, a.Location())
	return int(bd.Sub(ad).Hours() / 24)
}
