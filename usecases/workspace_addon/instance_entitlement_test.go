package workspace_addon_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	wsc "vozko/domain/workspace_config"
)

type fakeConfigReader struct {
	included int
	err      error
	calls    int
}

func (f *fakeConfigReader) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &wsc.WorkspaceConfig{IncludedUnofficialWhatsAppInstances: f.included}, nil
}

type fakeBatchConfigReader struct {
	included map[string]int
	err      error
}

func (f *fakeBatchConfigReader) GetIncludedUnofficialInstancesByWorkspaceIDs(
	context.Context, []string,
) (map[string]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.included, nil
}

func activeSubscription() *fakeCurrentSub {
	return &fakeCurrentSub{sub: &workspace_plan.WorkspaceSubscription{
		PlanDefinitionID: "plan-1",
		Status:           workspace_plan.SubscriptionStatusActive,
	}}
}

func TestInstanceBaseComesFromWorkspaceConfig(t *testing.T) {
	configs := &fakeConfigReader{included: 4}
	resolver := NewEntitlementResolver(
		activeSubscription(), &fakePlanReader{plan: &workspace_plan.PlanDefinition{}},
		newFakeSubRepo(), configs)

	total, err := resolver.Resolve("ws-1", workspace_addon.EntitlementUnofficialWhatsAppInstances)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want the 4 granted on the workspace config", total)
	}
	if configs.calls == 0 {
		t.Error("the workspace config was never read; the base must not come from the plan")
	}
}

func TestAddonsTopUpTheGrantedAllowance(t *testing.T) {
	addons := newFakeSubRepo(&workspace_addon.AddonSubscription{
		ID: "sub-1", WorkspaceID: "ws-1",
		EntitlementKind:  workspace_addon.EntitlementUnofficialWhatsAppInstances,
		Status:           workspace_plan.SubscriptionStatusActive,
		Quantity:         3,
		UnitsPerQuantity: 1,
	})

	resolver := NewEntitlementResolver(
		activeSubscription(), &fakePlanReader{plan: &workspace_plan.PlanDefinition{}},
		addons, &fakeConfigReader{included: 2})

	total, err := resolver.Resolve("ws-1", workspace_addon.EntitlementUnofficialWhatsAppInstances)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5 (2 granted + 3 purchased)", total)
	}
}

func TestGrantedAllowanceSurvivesAnInactivePlan(t *testing.T) {
	resolver := NewEntitlementResolver(
		&fakeCurrentSub{},
		&fakePlanReader{},
		newFakeSubRepo(), &fakeConfigReader{included: 3})

	total, err := resolver.Resolve("ws-1", workspace_addon.EntitlementUnofficialWhatsAppInstances)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want the 3 granted; a lapsed plan must not silently revoke a grant", total)
	}
}

func TestPlanDerivedKindsStillRequireAnActivePlan(t *testing.T) {
	resolver := NewEntitlementResolver(
		&fakeCurrentSub{}, &fakePlanReader{}, newFakeSubRepo(), &fakeConfigReader{included: 3})

	_, err := resolver.Resolve("ws-1", workspace_addon.EntitlementWhatsAppBusinessPhones)
	if !errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) {
		t.Errorf("err = %v, want ErrSubscriptionNotCurrent — the config base must not leak "+
			"into kinds that are plan-derived", err)
	}
}

func TestConfigReadFailurePropagates(t *testing.T) {
	boom := errors.New("database is unhappy")
	resolver := NewEntitlementResolver(
		activeSubscription(), &fakePlanReader{plan: &workspace_plan.PlanDefinition{}},
		newFakeSubRepo(), &fakeConfigReader{err: boom})

	if _, err := resolver.Resolve("ws-1", workspace_addon.EntitlementUnofficialWhatsAppInstances); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the underlying read failure", err)
	}
}

func TestEntitlementsListIncludesTheInstanceKind(t *testing.T) {
	uc := NewGetWorkspaceEntitlementsUseCase(
		activeSubscription(), &fakePlanReader{plan: &workspace_plan.PlanDefinition{}},
		newFakeSubRepo(), &fakeConfigReader{included: 7})

	ents, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, e := range ents {
		if e.Kind != workspace_addon.EntitlementUnofficialWhatsAppInstances {
			continue
		}
		if e.Total != 7 || e.PlanBase != 7 {
			t.Errorf("entitlement = %+v, want a base of 7 from the workspace config", e)
		}
		return
	}
	t.Fatal("the unofficial-whatsapp entitlement is absent from the list the gate reads")
}

func TestBatchResolverUsesTheConfigBase(t *testing.T) {
	resolver := NewBatchEntitlementResolver(
		&fakeBatchSubs{}, &fakePlanReader{}, &fakeBatchAddons{},
		&fakeBatchConfigReader{included: map[string]int{"ws-1": 2, "ws-2": 5}})

	out, err := resolver.ResolveMany(
		[]string{"ws-1", "ws-2", "ws-3"},
		workspace_addon.EntitlementUnofficialWhatsAppInstances)
	if err != nil {
		t.Fatalf("ResolveMany: %v", err)
	}
	if out["ws-1"] != 2 || out["ws-2"] != 5 {
		t.Errorf("out = %v, want the per-workspace grants", out)
	}
	if got, ok := out["ws-3"]; !ok || got != 0 {
		t.Errorf("ws-3 = %v (present=%v), want 0", got, ok)
	}
}

func TestInstanceKindIsSellable(t *testing.T) {
	kind := workspace_addon.EntitlementUnofficialWhatsAppInstances
	if !kind.IsValid() {
		t.Fatal("the kind is invalid; an addon definition for it could never be created")
	}

	for _, k := range workspace_addon.AllEntitlementKinds() {
		if k == kind {
			return
		}
	}
	t.Error("the kind is missing from AllEntitlementKinds, so it is absent from every " +
		"entitlement listing the product renders")
}

type fakeBatchSubs struct{}

func (f *fakeBatchSubs) GetCurrentByWorkspaceIDs([]string, time.Time) (map[string]*workspace_plan.WorkspaceSubscription, error) {
	return map[string]*workspace_plan.WorkspaceSubscription{}, nil
}

type fakeBatchAddons struct{}

func (f *fakeBatchAddons) SumActiveGrantedUnitsByWorkspaceIDs([]string, workspace_addon.EntitlementKind) (map[string]int, error) {
	return map[string]int{}, nil
}
