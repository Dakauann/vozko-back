package billing

import (
	"math"
	"time"
)

func MonthlyChargeBRL(planBRLCents int64, addonUSDMicros []int64, fxUSDToBRL float64) (totalBRL, creditableBRL float64) {
	planBRL := float64(planBRLCents) / 100.0
	var addonsBRL float64
	for _, usd := range addonUSDMicros {
		addonsBRL += float64(usd) / 1_000_000.0 * fxUSDToBRL
	}
	return roundCents(planBRL + addonsBRL), roundCents(planBRL)
}

func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

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
	if c > fullMicros {
		c = fullMicros
	}
	if c < 0 {
		c = 0
	}
	return c, first
}

func FirstBillingAnchor(at time.Time, emitDay, dueDay int) time.Time {
	atBRT := at.In(LocationBRT())
	if atBRT.Day() < emitDay {
		return anchorDate(atBRT.Year(), atBRT.Month(), dueDay, atBRT.Location())
	}
	y, m := addMonth(atBRT.Year(), atBRT.Month())
	return anchorDate(y, m, dueDay, atBRT.Location())
}

func ActivationProRataDays(at time.Time, emitDay, dueDay int) int {
	atBRT := at.In(LocationBRT())
	return daysBetweenDates(atBRT, FirstBillingAnchor(atBRT, emitDay, dueDay))
}

func daysBetweenDates(a, b time.Time) int {
	ad := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, a.Location())
	bd := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, a.Location())
	return int(bd.Sub(ad).Hours() / 24)
}
