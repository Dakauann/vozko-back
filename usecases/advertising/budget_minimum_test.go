package advertising

import (
	"context"
	"errors"
	"slices"
	"testing"

	ads "vozko/domain/advertising"
)

func brlMinimums() ads.MinimumBudgets {
	return ads.MinimumBudgets{Currency: "BRL", Account: 100, HighFrequency: 519}
}

func linkClicksDraft(amount int64) ads.AdDraft {
	d := publishableDraft()
	d.Campaign.Objective = ads.ObjectiveTraffic
	d.AdSet.Goal = ads.GoalLinkClicks
	d.AdSet.Budget = &ads.Budget{Kind: ads.BudgetDaily, Amount: amount}
	return d
}

func TestPreflightRefusesADailyBudgetBelowTheAccountMinimumBeforeAnyFee(t *testing.T) {
	w := newWorld()
	w.gateway.minimums = brlMinimums()
	_, err := publish(t, w, linkClicksDraft(500))
	var invalid *ads.ValidationError
	if !errors.As(err, &invalid) || !slices.Contains(invalid.Issues, ads.FieldIssue{Field: "adSet.budget.amount", Code: ads.CodeBelowMinimum}) {
		t.Fatalf("err %v", err)
	}
	if invalid.Minimum == nil || *invalid.Minimum != (ads.BudgetMinimum{Field: "adSet.budget.amount", Daily: 519, Currency: "BRL"}) {
		t.Fatalf("minimum %+v", invalid.Minimum)
	}
	if len(w.fees.charged) != 0 || len(metaWrites(w.gateway.calls)) != 0 {
		t.Fatalf("charged %v writes %v", w.fees.charged, metaWrites(w.gateway.calls))
	}
}

func TestPreflightReportsTheMinimumOfAnAcceptedBudget(t *testing.T) {
	w := newWorld()
	w.gateway.minimums = brlMinimums()
	pre, err := w.publisher().Preflight(context.Background(), "ws-1", linkClicksDraft(519))
	if err != nil {
		t.Fatal(err)
	}
	if pre.BudgetMinimum == nil || pre.BudgetMinimum.Daily != 519 {
		t.Fatalf("minimum %+v", pre.BudgetMinimum)
	}
}

func TestPreflightAsksForTheMinimumOfTheManualBid(t *testing.T) {
	w := newWorld()
	w.gateway.minimums = brlMinimums()
	d := linkClicksDraft(2000)
	d.AdSet.Bid = ads.Bid{Strategy: ads.BidCap, Amount: 300}
	if _, err := w.publisher().Preflight(context.Background(), "ws-1", d); err != nil {
		t.Fatal(err)
	}
	if w.gateway.minimumBid != 300 {
		t.Fatalf("bid %d", w.gateway.minimumBid)
	}
}

func TestPreflightDoesNotAskForMinimumsWithoutANewDailyAdSetBudget(t *testing.T) {
	w := newWorld()
	d := publishableDraft()
	d.AdSet.Budget = nil
	d.Campaign.Budget = &ads.Budget{Kind: ads.BudgetDaily, Amount: 1}
	w.publisher().Preflight(context.Background(), "ws-1", d)
	if slices.Contains(w.gateway.calls, "minimum_budgets") {
		t.Fatalf("calls %v", w.gateway.calls)
	}
}

func TestBudgetEditBelowTheMinimumNeverReachesMeta(t *testing.T) {
	w := newWorld()
	seedStructure(w)
	adSetWithBudget(w)
	w.objects.byID["s-1"].OptimizationGoal = string(ads.GoalLinkClicks)
	w.gateway.minimums = brlMinimums()
	_, err := w.manager().SetBudget(context.Background(), "ws-1", "s-1", 500)
	requireIssue(t, err, "budget.amount", ads.CodeBelowMinimum)
	if slices.Contains(w.gateway.calls, "update:s-1") {
		t.Fatal("budget below the minimum sent to meta")
	}
	if _, err := w.manager().CheckEdit(context.Background(), "ws-1", "s-1", ads.ObjectEdit{Budget: &ads.Budget{Kind: ads.BudgetDaily, Amount: 500}}); err == nil {
		t.Fatal("check passed a budget below the minimum")
	}
	if _, err := w.manager().SetBudget(context.Background(), "ws-1", "s-1", 519); err != nil {
		t.Fatalf("budget at the minimum refused: %v", err)
	}
}

func TestBudgetMinimumForTheWizard(t *testing.T) {
	w := newWorld()
	w.gateway.minimums = brlMinimums()
	got, err := NewAssetsUseCase(w.sync, w.gateway, w.numbers).BudgetMinimum(context.Background(), "ws-1", "acc-1", ads.GoalLinkClicks, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != (ads.BudgetMinimum{Daily: 519, Currency: "BRL"}) {
		t.Fatalf("got %+v", got)
	}
}
