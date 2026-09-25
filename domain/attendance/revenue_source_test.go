package attendance

import "testing"

func TestRevenueSplitsBySourceOfTheOwner(t *testing.T) {
	got := BuildRevenue([]RevenueTally{
		{Currency: "BRL", OwnerID: "u1", WonCount: 2, ValueCents: 30000},
		{Currency: "BRL", OwnerID: "u2", WonCount: 1, ValueCents: 10000},
		{Currency: "BRL", OwnerID: "ai:agent-1", WonCount: 3, ValueCents: 45000},
		{Currency: "BRL", OwnerID: "workflow:wf-1", WonCount: 1, ValueCents: 5000},
		{Currency: "BRL", OwnerID: "", WonCount: 1, ValueCents: 2000},
	}, 0, Period{}, nil)

	want := []RevenueSourceRow{
		{Source: RevenueSourceHuman, Currency: "BRL", WonCount: 3, ValueCents: 40000},
		{Source: RevenueSourceAI, Currency: "BRL", WonCount: 3, ValueCents: 45000},
		{Source: RevenueSourceWorkflow, Currency: "BRL", WonCount: 1, ValueCents: 5000},
		{Source: RevenueSourceUnowned, Currency: "BRL", WonCount: 1, ValueCents: 2000},
	}
	if len(got.BySource) != len(want) {
		t.Fatalf("BySource = %+v, want %+v", got.BySource, want)
	}
	for i := range want {
		if got.BySource[i] != want[i] {
			t.Fatalf("BySource[%d] = %+v, want %+v", i, got.BySource[i], want[i])
		}
	}
}

func TestRevenueSourcesNeverMixCurrencies(t *testing.T) {
	got := BuildRevenue([]RevenueTally{
		{Currency: "BRL", OwnerID: "ai:agent-1", WonCount: 1, ValueCents: 10000},
		{Currency: "USD", OwnerID: "ai:agent-1", WonCount: 1, ValueCents: 3000},
	}, 0, Period{}, nil)
	if len(got.BySource) != 2 || got.BySource[0].Currency == got.BySource[1].Currency {
		t.Fatalf("BySource = %+v, want one AI row per currency", got.BySource)
	}
}

func TestRevenueCountsWinsWithoutAValue(t *testing.T) {
	got := BuildRevenue([]RevenueTally{
		{Currency: "BRL", OwnerID: "u1", WonCount: 5, ValueCents: 30000, WonWithoutValue: 3},
		{Currency: "BRL", OwnerID: "ai:agent-1", WonCount: 2, ValueCents: 9000, WonWithoutValue: 1},
	}, 0, Period{}, nil)
	if got.WonWithoutValue != 4 {
		t.Fatalf("WonWithoutValue = %d, want 4", got.WonWithoutValue)
	}
}

func TestUnavailableRevenueStillCarriesItsLists(t *testing.T) {
	got := UnavailableRevenue(ReasonNoRevenueRepository)
	if got.BySource == nil {
		t.Fatalf("an unavailable revenue must still serialise an empty source list")
	}
}

func TestRevenueScopeFollowsTheConversationFilters(t *testing.T) {
	filter := OverviewFilter{CampaignID: "c1", CampaignType: "whatsapp", Channel: "whatsapp", DepartmentID: "d1", MemberID: "u1", RankMetric: "volume"}
	got := filter.RevenueScope()
	want := RevenueScope{CampaignID: "c1", CampaignType: "whatsapp", Channel: "whatsapp", DepartmentID: "d1"}
	if got != want {
		t.Fatalf("RevenueScope() = %+v, want %+v", got, want)
	}
	if got.Empty() {
		t.Fatalf("a scoped filter reported an empty scope")
	}
	if !(OverviewFilter{MemberID: "u1"}).RevenueScope().Empty() {
		t.Fatalf("a member filter is an owner filter, not a conversation scope")
	}
}
