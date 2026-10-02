package advertising

import (
	"errors"
	"testing"
)

func brlMinimums() MinimumBudgets {
	return MinimumBudgets{Currency: "BRL", Account: 100, Impressions: 120, VideoViews: 300, HighFrequency: 519, LowFrequency: 20_000}
}

func TestEachDocumentedGoalUsesItsOwnMinimum(t *testing.T) {
	m := brlMinimums()
	cases := map[OptimizationGoal]int64{
		GoalImpressions:    120,
		GoalThruPlay:       300,
		GoalLinkClicks:     519,
		GoalAppInstalls:    20_000,
		GoalConversations:  100,
		GoalLeadGeneration: 100,
	}
	for goal, want := range cases {
		if got := m.DailyFor(goal); got != want {
			t.Fatalf("%s: got %d, want %d", goal, got, want)
		}
	}
}

func TestTheAccountMinimumIsAFloorForEveryGoal(t *testing.T) {
	m := MinimumBudgets{Currency: "BRL", Account: 600, HighFrequency: 519}
	if got := m.DailyFor(GoalLinkClicks); got != 600 {
		t.Fatalf("got %d", got)
	}
}

func TestDailyBudgetBelowTheMinimumIsRefusedWithTheMinimum(t *testing.T) {
	minimum, err := brlMinimums().Check("adSet.budget.amount", Budget{Kind: BudgetDaily, Amount: 500}, GoalLinkClicks)
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("not refused: %v", err)
	}
	if len(invalid.Issues) != 1 || invalid.Issues[0] != (FieldIssue{Field: "adSet.budget.amount", Code: "below_minimum"}) {
		t.Fatalf("issues %+v", invalid.Issues)
	}
	want := BudgetMinimum{Field: "adSet.budget.amount", Daily: 519, Currency: "BRL"}
	if invalid.Minimum == nil || *invalid.Minimum != want || minimum != want {
		t.Fatalf("minimum %+v / %+v", invalid.Minimum, minimum)
	}
}

func TestDailyBudgetAtTheMinimumPasses(t *testing.T) {
	minimum, err := brlMinimums().Check("budget.amount", Budget{Kind: BudgetDaily, Amount: 519}, GoalLinkClicks)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if minimum.Daily != 519 {
		t.Fatalf("minimum %+v", minimum)
	}
}

func TestLifetimeBudgetsAreNotJudgedAgainstTheDailyMinimum(t *testing.T) {
	if _, err := brlMinimums().Check("adSet.budget.amount", Budget{Kind: BudgetLifetime, Amount: 1}, GoalLinkClicks); err != nil {
		t.Fatalf("lifetime refused: %v", err)
	}
}

func TestUnknownMinimumsRefuseNothing(t *testing.T) {
	if _, err := (MinimumBudgets{}).Check("adSet.budget.amount", Budget{Kind: BudgetDaily, Amount: 1}, GoalLinkClicks); err != nil {
		t.Fatalf("refused without a known minimum: %v", err)
	}
}

func TestOnlyManualBidsChangeTheMinimum(t *testing.T) {
	if got := (Bid{Strategy: BidCap, Amount: 300}).ManualAmount(); got != 300 {
		t.Fatalf("bid cap %d", got)
	}
	if got := (Bid{Strategy: BidLowestCost}).ManualAmount(); got != 0 {
		t.Fatalf("lowest cost %d", got)
	}
}
