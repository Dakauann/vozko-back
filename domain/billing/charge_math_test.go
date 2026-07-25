package billing

import (
	"testing"
	"time"
)

// brtDay builds a midday BRT time so the calendar day is unambiguous (midnight UTC would roll to the
// previous day in BRT and shift the proration).
func brtDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, LocationBRT())
}

func TestActivationPeriod_ChargeAndPeriodEnd(t *testing.T) {
	const monthly = int64(30_000_000) // $30/month in USD micros
	const emitDay, dueDay = 18, 23

	cases := []struct {
		name     string
		at       time.Time
		charge   int64
		endMonth time.Month
	}{
		// Before the emit day -> on THIS month's 23rd invoice; prorate [at, Jun 23] over the May23->Jun23
		// cycle (31 days). Jun 5: 18 days -> 30M*18/31.
		{"before emit day (Jun 5)", brtDay(2026, time.June, 5), 17_419_355, time.June},
		// Jun 10: 13 days -> 30M*13/31.
		{"before emit day (Jun 10)", brtDay(2026, time.June, 10), 12_580_645, time.June},
		// Between emit day and anchor -> missed this month's invoice, so next month's 23rd. Jun 20 -> Jul 23
		// is 33 days over a 30-day cycle -> would be >1 month, so CAPPED at one month.
		{"emit-anchor window is capped (Jun 20)", brtDay(2026, time.June, 20), monthly, time.July},
		// On the anchor -> next month's 23rd, exactly one cycle (30 days) -> a full month.
		{"on the anchor (Jun 23)", brtDay(2026, time.June, 23), monthly, time.July},
		// After the anchor -> next month's 23rd, a partial cycle. Jun 25 -> Jul 23 = 28 days / 30.
		{"after the anchor (Jun 25)", brtDay(2026, time.June, 25), 28_000_000, time.July},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			charge, end := ActivationPeriod(tc.at, emitDay, dueDay, 1, true, monthly)
			if charge != tc.charge {
				t.Fatalf("charge = %d, want %d", charge, tc.charge)
			}
			e := end.In(LocationBRT())
			if e.Day() != dueDay || e.Month() != tc.endMonth {
				t.Fatalf("period end = %s, want the %d of %s", e.Format("2006-01-02"), dueDay, tc.endMonth)
			}
		})
	}
}

// TestActivationPeriod_NeverMoreThanOneMonth is the core guarantee the fix must hold for every activation
// day of the month (31-day, 28-day, and leap February): the up-front charge is positive, never exceeds a
// full month, and always co-terms to a real anchor (day 23) so the recurring cycle has no gap.
func TestActivationPeriod_NeverMoreThanOneMonth(t *testing.T) {
	const monthly = int64(30_000_000)
	const emitDay, dueDay = 18, 23
	for _, ym := range []struct {
		y int
		m time.Month
	}{{2026, time.July}, {2026, time.February}, {2024, time.February}} {
		dim := daysInMonth(ym.y, ym.m, LocationBRT())
		for d := 1; d <= dim; d++ {
			at := brtDay(ym.y, ym.m, d)
			charge, end := ActivationPeriod(at, emitDay, dueDay, 1, true, monthly)
			if charge <= 0 || charge > monthly {
				t.Fatalf("%s: charge %d must be in (0, %d]", at.Format("2006-01-02"), charge, monthly)
			}
			if end.In(LocationBRT()).Day() != dueDay {
				t.Fatalf("%s: period end must be an anchor (day %d), got %s", at.Format("2006-01-02"), dueDay, end.In(LocationBRT()).Format("2006-01-02"))
			}
		}
	}
}

func TestActivationPeriod_AnnualAndTopUpChargeFullPeriod(t *testing.T) {
	const annual = int64(300_000_000)
	at := brtDay(2026, time.June, 25)
	// Annual: full amount, +12 months, no proration.
	if charge, end := ActivationPeriod(at, 18, 23, 12, true, annual); charge != annual || end.Year() != 2027 {
		t.Fatalf("annual = (%d, %s), want (%d, +12 months)", charge, end.Format("2006-01-02"), annual)
	}
	// Monthly top-up of an existing addon (isNew=false): full month, +1 month, no proration.
	const monthly = int64(30_000_000)
	if charge, _ := ActivationPeriod(at, 18, 23, 1, false, monthly); charge != monthly {
		t.Fatalf("top-up charge = %d, want the full month %d", charge, monthly)
	}
}

func TestMonthlyChargeBRL(t *testing.T) {
	const fx = 6.0 // 6 BRL per USD

	cases := []struct {
		name           string
		planBRLCents   int64
		addonUSDMicros []int64
		fx             float64
		wantTotal      float64
		wantCreditable float64
	}{
		{
			// Plan R$1.099, no channels: total == creditable == plan.
			name:         "plan only",
			planBRLCents: 109_900, addonUSDMicros: nil, fx: fx,
			wantTotal: 1099.00, wantCreditable: 1099.00,
		},
		{
			// Plan R$1.099 + two $25 channels = R$1.099 + R$150 + R$150 = R$1.399; only plan creditable.
			name:         "plan plus two channels",
			planBRLCents: 109_900, addonUSDMicros: []int64{25_000_000, 25_000_000}, fx: fx,
			wantTotal: 1399.00, wantCreditable: 1099.00,
		},
		{
			// No plan (channels-only edge): nothing becomes saldo.
			name:         "channels only credits nothing",
			planBRLCents: 0, addonUSDMicros: []int64{30_000_000}, fx: fx,
			wantTotal: 180.00, wantCreditable: 0.00,
		},
		{
			// FX changes the channel portion but never the plan portion.
			name:         "higher fx raises only the channel portion",
			planBRLCents: 109_900, addonUSDMicros: []int64{25_000_000}, fx: 6.50,
			wantTotal: 1099.00 + 162.50, wantCreditable: 1099.00,
		},
		{
			// Rounding to cents: $0.005 (5000 micros) at fx 6.0 = R$0.03.
			name:         "rounds to cents",
			planBRLCents: 1000, addonUSDMicros: []int64{5_000}, fx: fx,
			wantTotal: 10.03, wantCreditable: 10.00,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			total, creditable := MonthlyChargeBRL(tc.planBRLCents, tc.addonUSDMicros, tc.fx)
			if total != tc.wantTotal {
				t.Errorf("total = %.2f, want %.2f", total, tc.wantTotal)
			}
			if creditable != tc.wantCreditable {
				t.Errorf("creditable = %.2f, want %.2f", creditable, tc.wantCreditable)
			}
			if creditable > total {
				t.Errorf("creditable (%.2f) must never exceed total (%.2f)", creditable, total)
			}
		})
	}
}
