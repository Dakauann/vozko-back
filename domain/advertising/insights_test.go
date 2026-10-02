package advertising

import (
	"errors"
	"testing"
)

func TestResultsFollowTheAdSetOptimizationGoal(t *testing.T) {
	row := DailyInsight{
		Currency: "BRL", SpendMicros: 50_000_000, Impressions: 4000, LinkClicks: 80,
		Actions: map[string]int64{ActionConversationStarted: 10, ActionLead: 2, ActionLinkClick: 80},
	}
	cases := map[string]int64{"CONVERSATIONS": 10, "LEAD_GENERATION": 2, "LINK_CLICKS": 80, "IMPRESSIONS": 4000}
	for goal, want := range cases {
		if got := MetricsOf(row, goal).Results; got != want {
			t.Fatalf("%s: results %d, want %d", goal, got, want)
		}
	}
}

func TestUnknownGoalHasNoResultsAndNoCostPerResult(t *testing.T) {
	m := MetricsOf(DailyInsight{Currency: "BRL", SpendMicros: 10_000_000, Impressions: 100}, "SOMETHING_NEW")
	if m.Results != 0 || m.CostPerResult() != nil {
		t.Fatalf("guessed a result: %+v", m)
	}
}

func TestCostPerResultDividesSpendByResults(t *testing.T) {
	m := MetricsOf(DailyInsight{Currency: "BRL", SpendMicros: 355_440_000, Actions: map[string]int64{ActionConversationStarted: 18}}, "CONVERSATIONS")
	if got := *m.CostPerResult(); got != 19_746_666 {
		t.Fatalf("cost per result %d", got)
	}
	if got := *m.CostPerConversation(); got != 19_746_666 {
		t.Fatalf("cost per conversation %d", got)
	}
}

func TestZeroDenominatorsGiveNoNumberRatherThanZero(t *testing.T) {
	m := Metrics{Currency: "BRL", SpendMicros: 5_000_000, ResultAction: ActionConversationStarted}
	if m.CostPerResult() != nil || m.CPM() != nil || m.CTR() != nil || m.CostPerLinkClick() != nil {
		t.Fatal("division by zero produced a number")
	}
}

func TestSummingDifferentGoalsMarksResultsMixed(t *testing.T) {
	a := MetricsOf(DailyInsight{Currency: "BRL", SpendMicros: 1, Actions: map[string]int64{ActionConversationStarted: 3}}, "CONVERSATIONS")
	b := MetricsOf(DailyInsight{Currency: "BRL", SpendMicros: 1, Actions: map[string]int64{ActionLinkClick: 9}}, "LINK_CLICKS")
	sum, err := SumMetrics([]Metrics{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !sum.MixedResults || sum.Results != 0 || sum.CostPerResult() != nil {
		t.Fatalf("mixed goals summed: %+v", sum)
	}
	if sum.Conversations != 3 {
		t.Fatalf("conversations %d", sum.Conversations)
	}
}

func TestSummingSameGoalAddsResults(t *testing.T) {
	a := MetricsOf(DailyInsight{Currency: "BRL", SpendMicros: 10, Impressions: 1, Actions: map[string]int64{ActionConversationStarted: 3}}, "CONVERSATIONS")
	sum, err := SumMetrics([]Metrics{a, a, {}})
	if err != nil || sum.Results != 6 || sum.SpendMicros != 20 || sum.MixedResults {
		t.Fatalf("sum %+v, %v", sum, err)
	}
}

func TestSpendInDifferentCurrenciesIsNeverAdded(t *testing.T) {
	_, err := SumMetrics([]Metrics{{Currency: "BRL", SpendMicros: 1}, {Currency: "USD", SpendMicros: 1}})
	if !errors.Is(err, ErrMixedCurrencies) {
		t.Fatalf("got %v", err)
	}
}

func TestCTRUsesLinkClicksLikeMeta(t *testing.T) {
	m := Metrics{Currency: "BRL", Impressions: 2000, LinkClicks: 30, Clicks: 90}
	if got := *m.CTR(); got != 1.5 {
		t.Fatalf("ctr %v", got)
	}
}
