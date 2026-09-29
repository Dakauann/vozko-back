package workspace_plan_usecase

import (
	"errors"
	"testing"
	"time"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type stubSubscriptionReader struct {
	sub *workspace_plan.WorkspaceSubscription
	err error
}

func (s stubSubscriptionReader) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return s.sub, s.err
}

type stubPlanItems struct {
	workspace_plan.PlanRepository
	items []workspace_plan.PlanPricingItem
	err   error
}

func (s stubPlanItems) ListPricingItems(string) ([]workspace_plan.PlanPricingItem, error) {
	return s.items, s.err
}

func TestPlanPricingUsesDefaultsOnlyWhenThereIsNoCurrentPlan(t *testing.T) {
	adapter := NewPlanPricingAdapter(stubSubscriptionReader{err: workspace_plan.ErrSubscriptionNotCurrent}, stubPlanItems{})
	items, err := adapter.ListForWorkspace("ws-1")
	if err != nil || items != nil {
		t.Fatalf("no current plan = %v, %v, want no plan items and no error", items, err)
	}
}

func TestPlanPricingSurfacesLookupFailures(t *testing.T) {
	outage := errors.New("database unavailable")
	adapter := NewPlanPricingAdapter(stubSubscriptionReader{err: outage}, stubPlanItems{})
	if _, err := adapter.ListForWorkspace("ws-1"); !errors.Is(err, outage) {
		t.Fatalf("subscription lookup failure = %v, want the failure so pricing fails closed", err)
	}
	adapter = NewPlanPricingAdapter(
		stubSubscriptionReader{sub: &workspace_plan.WorkspaceSubscription{PlanDefinitionID: "plan-1"}},
		stubPlanItems{err: outage},
	)
	if _, err := adapter.ListForWorkspace("ws-1"); !errors.Is(err, outage) {
		t.Fatalf("plan item lookup failure = %v, want the failure", err)
	}
}

func TestPlanPricingMapsThePlanLines(t *testing.T) {
	adapter := NewPlanPricingAdapter(
		stubSubscriptionReader{sub: &workspace_plan.WorkspaceSubscription{PlanDefinitionID: "plan-1"}},
		stubPlanItems{items: []workspace_plan.PlanPricingItem{{Category: "telephony", Service: "sip_calls", Metric: "per_minute", PriceMicros: 5_000, Currency: "USD"}}},
	)
	items, err := adapter.ListForWorkspace("ws-1")
	if err != nil || len(items) != 1 || items[0].Service != "sip_calls" || items[0].PriceMicros != 5_000 {
		t.Fatalf("ListForWorkspace() = %+v, %v", items, err)
	}
}
