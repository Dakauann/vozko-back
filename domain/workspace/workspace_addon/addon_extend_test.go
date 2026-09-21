package workspace_addon

import (
	"testing"
	"time"

	billing "vozko/domain/billing"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

func TestAddonSubscription_Extend_MonthlyConvergesToAnchor(t *testing.T) {
	from := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	for _, day := range []int{5, 12, 27} {
		s := &AddonSubscription{
			BillingCycle:     workspace_plan.BillingCycleMonthly,
			Status:           workspace_plan.SubscriptionStatusExpired,
			CurrentPeriodEnd: time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC),
		}
		s.Extend(from, billing.DefaultDueDay)
		if got := s.CurrentPeriodEnd.In(billing.LocationBRT()).Day(); got != billing.DefaultDueDay {
			t.Fatalf("addon monthly Extend from day %d landed on day %d, want %d", day, got, billing.DefaultDueDay)
		}
		if s.Status != workspace_plan.SubscriptionStatusActive {
			t.Fatalf("Extend must revive the addon to active, got %s", s.Status)
		}
	}
}

func TestActivation_NoGap_CoverageIsContinuous(t *testing.T) {
	const emitDay, dueDay = 18, 23
	for _, day := range []int{5, 20, 23, 25, 28} {
		at := time.Date(2026, time.June, day, 12, 0, 0, 0, billing.LocationBRT())
		_, firstAnchor := billing.ActivationPeriod(at, emitDay, dueDay, 1, true, 30_000_000)

		sub := &AddonSubscription{
			BillingCycle:     workspace_plan.BillingCycleMonthly,
			CurrentPeriodEnd: firstAnchor,
		}
		sub.Extend(firstAnchor, dueDay)

		if !sub.CurrentPeriodStart.Equal(firstAnchor) {
			t.Fatalf("day %d: recurring period starts %s, not the activation end %s -> COVERAGE GAP",
				day, sub.CurrentPeriodStart.Format("2006-01-02"), firstAnchor.Format("2006-01-02"))
		}
		if sub.CurrentPeriodEnd.In(billing.LocationBRT()).Day() != dueDay {
			t.Fatalf("day %d: next period must end on an anchor, got %s", day, sub.CurrentPeriodEnd.Format("2006-01-02"))
		}
	}
}

func TestAddonSubscription_Extend_AnnualRollsTwelveMonths(t *testing.T) {
	from := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	s := &AddonSubscription{BillingCycle: workspace_plan.BillingCycleAnnual, CurrentPeriodEnd: from}
	s.Extend(from, billing.DefaultDueDay)
	if want := from.AddDate(0, 12, 0); !s.CurrentPeriodEnd.Equal(want) {
		t.Fatalf("annual addon Extend = %s, want +12 months (%s)", s.CurrentPeriodEnd, want)
	}
}
