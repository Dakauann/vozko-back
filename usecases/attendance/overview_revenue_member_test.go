package attendance_usecase

import (
	"testing"

	"vozko/domain/attendance"
)

func TestOverviewRevenueIsScopedToTheFilteredOperator(t *testing.T) {
	tallies := []attendance.RevenueTally{
		{Currency: "BRL", OwnerID: "u1", WonCount: 1, ValueCents: 23_300},
		{Currency: "BRL", OwnerID: "u2", WonCount: 9, ValueCents: 900_000},
	}

	scoped := attendance.RevenueForOwner(tallies, "u1")
	revenue := attendance.BuildRevenue(scoped, 0, attendance.Period{
		Available: true, OpenDaysTotal: 22, OpenDaysDone: 17,
	}, nil)

	if len(revenue.Currencies) != 1 {
		t.Fatalf("currencies = %+v", revenue.Currencies)
	}
	if revenue.Currencies[0].ValueCents != 23_300 {
		t.Fatalf("value = %d cents; filtering by one operator must not report the team's revenue",
			revenue.Currencies[0].ValueCents)
	}
	if revenue.Currencies[0].WonCount != 1 {
		t.Fatalf("won = %d, want only that operator's deals", revenue.Currencies[0].WonCount)
	}
}

func operatorRepo() *stubOverviewRepo {
	return &stubOverviewRepo{
		revenue: []attendance.RevenueTally{
			{Currency: "BRL", OwnerID: "u1", WonCount: 1, ValueCents: 23_300},
			{Currency: "BRL", OwnerID: "u2", WonCount: 9, ValueCents: 900_000},
			{Currency: "BRL", OwnerID: "", WonCount: 4, ValueCents: 400_000},
		},
		revenueByMonth: []attendance.RevenueMonthRow{
			{Bucket: "2026-08", Currency: "BRL", ValueCents: 10_000, WonCount: 1},
		},
	}
}

func TestExecuteScopesRevenueToTheFilteredOperator(t *testing.T) {
	repo := operatorRepo()
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	out, err := uc.Execute("ws1", attendance.OverviewFilter{MemberID: "u1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Revenue.Available || len(out.Revenue.Currencies) != 1 {
		t.Fatalf("revenue = %+v", out.Revenue)
	}
	if got := out.Revenue.Currencies[0].ValueCents; got != 23_300 {
		t.Fatalf("revenue = %d cents, want only u1's deals, not the workspace's", got)
	}
	if out.Revenue.Unattributed != 0 {
		t.Fatalf("unattributed = %d; deals with no owner are not this operator's",
			out.Revenue.Unattributed)
	}
}

func TestExecuteAsksForTheOperatorsOwnMonthlyBaseline(t *testing.T) {
	repo := operatorRepo()
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	if _, err := uc.Execute("ws1", attendance.OverviewFilter{MemberID: "u1"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.monthOwners) == 0 {
		t.Fatal("the monthly revenue query was never asked for")
	}
	for _, owner := range repo.monthOwners {
		if owner != "u1" {
			t.Fatalf("monthly revenue was read for %q while filtered to u1; "+
				"the baseline would be the whole workspace", owner)
		}
	}
}

func TestExecuteGivesTheOperatorARealMonthOverMonthDelta(t *testing.T) {
	repo := operatorRepo()
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	out, err := uc.Execute("ws1", attendance.OverviewFilter{MemberID: "u1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Revenue.Currencies[0].PrevClosed == nil {
		t.Fatal("the operator card has no previous month to compare against")
	}
	if *out.Revenue.Currencies[0].PrevClosed != 10_000 {
		t.Fatalf("previous = %d, want the operator's own prior month",
			*out.Revenue.Currencies[0].PrevClosed)
	}
	if out.Revenue.Currencies[0].DeltaPct == nil {
		t.Fatal("a per-operator card should now carry its own delta")
	}
}

func TestExecuteReadsWorkspaceRevenueWhenNoOperatorIsFiltered(t *testing.T) {
	repo := operatorRepo()
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	out, err := uc.Execute("ws1", attendance.OverviewFilter{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := out.Revenue.Currencies[0].ValueCents; got != 1_323_300 {
		t.Fatalf("workspace revenue = %d, want every owner's deals summed", got)
	}
	for _, owner := range repo.monthOwners {
		if owner != "" {
			t.Fatalf("monthly revenue was scoped to %q with no operator filter", owner)
		}
	}
}

func trendRepoForOperator() *stubOverviewRepo {
	repo := operatorRepo()
	repo.trend = attendance.TrendResult{
		Buckets: []attendance.TrendBucketRow{
			{Bucket: "2026-08", Finished: 2, Engaged: 3, Created: 4, Pending: 1},
			{Bucket: "2026-09", Finished: 5, Engaged: 6, Created: 7, Pending: 2},
		},
	}
	return repo
}

func revenueSeriesOf(out *attendance.Overview) (attendance.TrendSeries, bool) {
	for _, series := range out.Trend.Series {
		if series.MetricKey == attendance.MetricRevenueCents {
			return series, true
		}
	}
	return attendance.TrendSeries{}, false
}

func TestRevenueTrendIsScopedToTheFilteredOperator(t *testing.T) {
	repo := trendRepoForOperator()
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	out, err := uc.Execute("ws1", attendance.OverviewFilter{MemberID: "u1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	series, found := revenueSeriesOf(out)
	if !found {
		t.Fatal("the operator view lost its revenue trend series entirely")
	}
	if len(series.Points) == 0 {
		t.Fatal("the revenue series has no points")
	}

	if len(repo.monthOwners) < 2 {
		t.Fatalf("monthly revenue was read %d time(s); the card and the trend both need it",
			len(repo.monthOwners))
	}
	for _, owner := range repo.monthOwners {
		if owner != "u1" {
			t.Fatalf("the revenue trend was read for %q while filtered to u1; "+
				"the series would show the whole workspace", owner)
		}
	}
}

func TestRevenueTrendStaysWorkspaceWideWithoutAnOperatorFilter(t *testing.T) {
	repo := trendRepoForOperator()
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	if _, err := uc.Execute("ws1", attendance.OverviewFilter{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, owner := range repo.monthOwners {
		if owner != "" {
			t.Fatalf("the revenue trend was scoped to %q with no operator filter", owner)
		}
	}
}
