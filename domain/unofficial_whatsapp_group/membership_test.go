package unofficial_whatsapp_group

import (
	"testing"

	"vozko/domain/shared"
)

func membershipRows() []MembershipStat {
	return []MembershipStat{
		{GroupID: "a", Subject: "Ofertas VIP #1", Members: 1018, Base: 1006, Joins: 30, Leaves: 18, Messages: 2140},
		{GroupID: "b", Subject: "Moradores Vila Mariana", Members: 287, Base: 293, Joins: 19, Leaves: 25, Messages: 3305},
		{GroupID: "c", Subject: "Feira de Trocas", Members: 12, Base: 0, Joins: 12, Leaves: 0, Messages: 40},
	}
}

func TestBuildMembershipPageComputesNetAndChurnWithoutABaseAsNoValue(t *testing.T) {
	page := BuildMembershipPage(membershipRows(), MembershipByName, false, shared.Pagination{Page: 1, PageSize: 10})
	byID := map[string]*MembershipStat{}
	for _, s := range page.Page.Items {
		byID[s.GroupID] = s
	}
	if byID["b"].Net != -6 || byID["a"].Net != 12 {
		t.Fatalf("net = %d / %d", byID["b"].Net, byID["a"].Net)
	}
	if byID["c"].ChurnRate != nil {
		t.Fatalf("a group that started the period empty has a churn rate %v", *byID["c"].ChurnRate)
	}
	if byID["b"].ChurnRate == nil || *byID["b"].ChurnRate < 8.53 || *byID["b"].ChurnRate > 8.54 {
		t.Fatalf("churn = %v, want 25/293", byID["b"].ChurnRate)
	}
}

func TestBuildMembershipPageSummarisesEveryRowNotJustThePage(t *testing.T) {
	page := BuildMembershipPage(membershipRows(), MembershipByMembers, true, shared.Pagination{Page: 1, PageSize: 1})
	if len(page.Page.Items) != 1 || page.Page.TotalItems != 3 {
		t.Fatalf("page = %d of %d", len(page.Page.Items), page.Page.TotalItems)
	}
	if page.Summary.Members != 1317 || page.Summary.Joins != 61 || page.Summary.Leaves != 43 || page.Summary.Net != 18 || page.Summary.Messages != 5485 {
		t.Fatalf("summary = %+v", page.Summary)
	}
}

func TestBuildMembershipPageSortsByTheChosenColumn(t *testing.T) {
	cases := []struct {
		sort  MembershipSort
		desc  bool
		first string
	}{
		{MembershipByMembers, true, "a"},
		{MembershipByMessages, true, "b"},
		{MembershipByNet, false, "b"},
		{MembershipByChurn, true, "b"},
		{MembershipByName, false, "c"},
		{MembershipByJoins, true, "a"},
		{MembershipByLeaves, true, "b"},
	}
	for _, c := range cases {
		page := BuildMembershipPage(membershipRows(), c.sort, c.desc, shared.Pagination{Page: 1, PageSize: 10})
		if got := page.Page.Items[0].GroupID; got != c.first {
			t.Fatalf("sort %s desc=%v first = %s, want %s", c.sort, c.desc, got, c.first)
		}
	}
}

func TestChurnSortKeepsGroupsWithoutABaseLast(t *testing.T) {
	for _, desc := range []bool{true, false} {
		page := BuildMembershipPage(membershipRows(), MembershipByChurn, desc, shared.Pagination{Page: 1, PageSize: 10})
		if last := page.Page.Items[len(page.Page.Items)-1].GroupID; last != "c" {
			t.Fatalf("desc=%v last = %s, want the group without a base", desc, last)
		}
	}
}

func TestMembershipCarriesGrowthAndTheWholeSetSummary(t *testing.T) {
	rows := []MembershipStat{
		{GroupID: "a", Subject: "Alfa", Members: 110, Base: 100, Joins: 20, Leaves: 10},
		{GroupID: "b", Subject: "Beta", Members: 45, Base: 50, Joins: 0, Leaves: 5},
	}
	page := BuildMembershipPage(rows, MembershipByGrowth, true, shared.Pagination{Page: 1, PageSize: 1})
	first := page.Page.Items[0]
	if first.GroupID != "a" || first.GrowthRate == nil || *first.GrowthRate != 10 {
		t.Fatalf("first = %+v, want Alfa growing 10%%", first)
	}
	sum := page.Summary
	if sum.Groups != 2 || sum.StartMembers != 150 || sum.ChurnRate == nil || *sum.ChurnRate != 10 || sum.GrowthRate == nil || *sum.GrowthRate < 3.33 || *sum.GrowthRate > 3.34 {
		t.Fatalf("summary = %+v, want 2 groups, 150 at the start, 10%% churn and 3.33%% growth", sum)
	}
}
