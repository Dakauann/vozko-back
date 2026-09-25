package attendance

import "testing"

func TestBuildRevenueSingleCurrency(t *testing.T) {
	tallies := []RevenueTally{
		{Currency: "BRL", OwnerID: "u1", WonCount: 4, ValueCents: 400_00},
		{Currency: "BRL", OwnerID: "u2", WonCount: 1, ValueCents: 100_00},
	}
	got := BuildRevenue(tallies, 0, maturePeriod(), nil)

	if !got.Available {
		t.Fatalf("BuildRevenue() Available = false, want true")
	}
	if got.MixedCurrencies {
		t.Fatalf("BuildRevenue() MixedCurrencies = true, want false")
	}
	if len(got.Currencies) != 1 || got.Currencies[0].ValueCents != 500_00 {
		t.Fatalf("BuildRevenue() currencies = %+v, want one BRL row at 50000", got.Currencies)
	}
	if got.Currencies[0].AvgTicket == nil || *got.Currencies[0].AvgTicket != 10000 {
		t.Fatalf("BuildRevenue() AvgTicket = %v, want 10000", got.Currencies[0].AvgTicket)
	}
	if got.SingleCurrencyCents() != 500_00 {
		t.Fatalf("SingleCurrencyCents() = %d, want 50000", got.SingleCurrencyCents())
	}
	if got.WonCount() != 5 {
		t.Fatalf("WonCount() = %d, want 5", got.WonCount())
	}
}

func TestBuildRevenueNeverSumsAcrossCurrencies(t *testing.T) {
	tallies := []RevenueTally{
		{Currency: "BRL", OwnerID: "u1", WonCount: 2, ValueCents: 200_00},
		{Currency: "USD", OwnerID: "u1", WonCount: 1, ValueCents: 900_00},
	}
	got := BuildRevenue(tallies, 0, maturePeriod(), nil)

	if !got.MixedCurrencies {
		t.Fatalf("BuildRevenue() MixedCurrencies = false, want true")
	}
	if got.Reason != ReasonRevenueMixedCurrencies {
		t.Fatalf("BuildRevenue() Reason = %q, want %q", got.Reason, ReasonRevenueMixedCurrencies)
	}
	if got.SingleCurrencyCents() != 0 {
		t.Fatalf("SingleCurrencyCents() across currencies = %d, want 0", got.SingleCurrencyCents())
	}
	if len(got.Currencies) != 2 {
		t.Fatalf("BuildRevenue() produced %d currency rows, want 2", len(got.Currencies))
	}
}

func TestBuildRevenueKeepsTheUnownedBucketSeparate(t *testing.T) {
	tallies := []RevenueTally{
		{Currency: "BRL", OwnerID: "", WonCount: 3, ValueCents: 300_00},
		{Currency: "BRL", OwnerID: "u1", WonCount: 1, ValueCents: 100_00},
	}
	got := BuildRevenue(tallies, 0, maturePeriod(), nil)

	if got.UnownedCount != 3 {
		t.Fatalf("BuildRevenue() UnownedCount = %d, want 3", got.UnownedCount)
	}
	for _, row := range got.ByOwner {
		if row.OwnerID == "" && row.ValueCents != 300_00 {
			t.Fatalf("BuildRevenue() unowned row = %+v, want 30000 cents kept in its own bucket", row)
		}
	}
}

func TestBuildRevenueReportsTheUnattributedCount(t *testing.T) {
	got := BuildRevenue(nil, 7, maturePeriod(), nil)
	if got.Unattributed != 7 {
		t.Fatalf("BuildRevenue() Unattributed = %d, want 7", got.Unattributed)
	}
	if len(got.Currencies) != 0 {
		t.Fatalf("BuildRevenue() with no tallies produced %d currency rows, want 0", len(got.Currencies))
	}
}

func TestBuildRevenueProjectsOnlyWithAMaturePeriod(t *testing.T) {
	tallies := []RevenueTally{{Currency: "BRL", OwnerID: "u1", WonCount: 19, ValueCents: 124_732_95}}

	projected := BuildRevenue(tallies, 0, maturePeriod(), nil)
	if projected.Currencies[0].Projected == nil {
		t.Fatalf("BuildRevenue() Projected = nil, want a projection for a mature period")
	}

	thin := BuildRevenue(tallies, 0, Period{Available: true, OpenDaysTotal: 21, OpenDaysDone: 0, ElapsedPct: 1}, nil)
	if thin.Currencies[0].Projected != nil {
		t.Fatalf("BuildRevenue() Projected = %v, want nil for a thin period", *thin.Currencies[0].Projected)
	}

	noPeriod := BuildRevenue(tallies, 0, Period{Reason: ReasonNoSchedule}, nil)
	if noPeriod.Currencies[0].PerOpenDay != nil {
		t.Fatalf("BuildRevenue() PerOpenDay = %v, want nil without a period", *noPeriod.Currencies[0].PerOpenDay)
	}
}

func TestBuildRevenueDeltaAgainstThePreviousMonth(t *testing.T) {
	tallies := []RevenueTally{{Currency: "BRL", OwnerID: "u1", WonCount: 10, ValueCents: 1000}}
	period := Period{Available: true, OpenDaysTotal: 10, OpenDaysDone: 10, ElapsedPct: 100}

	got := BuildRevenue(tallies, 0, period, map[string]int64{"BRL": 800})
	row := got.Currencies[0]
	if row.PrevClosed == nil || *row.PrevClosed != 800 {
		t.Fatalf("BuildRevenue() PrevClosed = %v, want 800", row.PrevClosed)
	}
	if row.DeltaPct == nil || *row.DeltaPct != 25 {
		t.Fatalf("BuildRevenue() DeltaPct = %v, want 25", row.DeltaPct)
	}
}

func TestBuildRevenueZeroPreviousMonthYieldsNoDelta(t *testing.T) {
	tallies := []RevenueTally{{Currency: "BRL", OwnerID: "u1", WonCount: 1, ValueCents: 1000}}
	got := BuildRevenue(tallies, 0, maturePeriod(), map[string]int64{"BRL": 0})

	if got.Currencies[0].DeltaPct != nil {
		t.Fatalf("BuildRevenue() DeltaPct = %v, want nil against a zero previous month", *got.Currencies[0].DeltaPct)
	}
}

func TestUnavailableRevenueCarriesItsReason(t *testing.T) {
	got := UnavailableRevenue(ReasonNoRevenueRepository)
	if got.Available {
		t.Fatalf("UnavailableRevenue() Available = true, want false")
	}
	if got.Reason != ReasonNoRevenueRepository {
		t.Fatalf("UnavailableRevenue() Reason = %q, want %q", got.Reason, ReasonNoRevenueRepository)
	}
	if got.Currencies == nil || got.ByOwner == nil {
		t.Fatalf("UnavailableRevenue() left a nil slice on the wire")
	}
}
