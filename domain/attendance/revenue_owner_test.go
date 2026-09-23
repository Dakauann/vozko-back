package attendance

import "testing"

func ownerTallies() []RevenueTally {
	return []RevenueTally{
		{Currency: "BRL", OwnerID: "u1", WonCount: 3, ValueCents: 90_000},
		{Currency: "BRL", OwnerID: "u2", WonCount: 5, ValueCents: 250_000},
		{Currency: "BRL", OwnerID: "", WonCount: 2, ValueCents: 40_000},
	}
}

func TestRevenueForOwnerKeepsOnlyThatOperator(t *testing.T) {
	got := RevenueForOwner(ownerTallies(), "u1")

	if len(got) != 1 {
		t.Fatalf("got %d tallies, want only the operator's", len(got))
	}
	if got[0].ValueCents != 90_000 || got[0].WonCount != 3 {
		t.Fatalf("tally = %+v", got[0])
	}
}

func TestRevenueForOwnerExcludesUnownedDeals(t *testing.T) {
	for _, owner := range []string{"u1", "u2"} {
		for _, tally := range RevenueForOwner(ownerTallies(), owner) {
			if tally.OwnerID == "" {
				t.Fatalf("owner %q picked up an unowned deal; that revenue is nobody's", owner)
			}
		}
	}
}

func TestRevenueForOwnerWithoutAnOwnerKeepsEverything(t *testing.T) {
	if len(RevenueForOwner(ownerTallies(), "")) != 3 {
		t.Fatal("an unfiltered view must keep every tally")
	}
}

func TestRevenueForOneOperatorNeverReportsTheWholeWorkspace(t *testing.T) {
	period := Period{Available: true, OpenDaysTotal: 20, OpenDaysDone: 10}

	everyone := BuildRevenue(ownerTallies(), 2, period, nil)
	justU1 := BuildRevenue(RevenueForOwner(ownerTallies(), "u1"), 0, period, nil)

	if len(everyone.Currencies) == 0 || len(justU1.Currencies) == 0 {
		t.Fatal("both views should report BRL")
	}
	if justU1.Currencies[0].ValueCents >= everyone.Currencies[0].ValueCents {
		t.Fatalf("one operator reported %d cents against the workspace's %d; the filter did nothing",
			justU1.Currencies[0].ValueCents, everyone.Currencies[0].ValueCents)
	}
	if justU1.Currencies[0].ValueCents != 90_000 {
		t.Fatalf("operator revenue = %d, want only their own deals", justU1.Currencies[0].ValueCents)
	}
}

func TestOperatorRevenueCarriesNoWorkspaceDelta(t *testing.T) {
	period := Period{Available: true, OpenDaysTotal: 20, OpenDaysDone: 10}

	justU1 := BuildRevenue(RevenueForOwner(ownerTallies(), "u1"), 0, period, nil)

	if justU1.Currencies[0].DeltaPct != nil {
		t.Fatal("a per-operator card must not borrow the workspace's month-over-month delta")
	}
}
