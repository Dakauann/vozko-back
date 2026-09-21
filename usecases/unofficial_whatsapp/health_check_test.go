package unofficial_whatsapp

import (
	"context"
	"strings"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

func TestSessionBackstopOnlyProbesStaleInstances(t *testing.T) {
	instances := newFakeInstanceRepo()
	var askedFor time.Time
	instances.ListForHealthCheckFn = func(before time.Time) ([]*uw.Instance, error) {
		askedFor = before
		return nil, nil
	}
	provider := &fakeProvider{
		StatusFn: func(context.Context, uw.InstanceRef) (*uw.Session, error) {
			t.Error("no host call may be made when nothing has gone stale")
			return nil, nil
		},
	}

	uc := NewCheckInstanceHealthUseCase(instances, newFakeServerRepo(), provider, testWebhookBase)
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if askedFor.IsZero() {
		t.Fatal("the backstop must bound its work by staleness")
	}
	if window := time.Since(askedFor); window < time.Minute {
		t.Errorf("staleness window is %v; too tight to let the connection webhook report first", window)
	}
}

func TestSessionBackstopDoesNotRunTheIntegrityProbes(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	instances := newFakeInstanceRepo(instance)
	instances.ListForHealthCheckFn = func(time.Time) ([]*uw.Instance, error) {
		return []*uw.Instance{instance}, nil
	}

	provider := &fakeProvider{
		GetWebhooksFn: func(context.Context, uw.InstanceRef) ([]uw.WebhookSubscription, error) {
			t.Error("the session backstop must not verify webhook registration; that is the hourly sweep")
			return nil, nil
		},
	}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(instances.sessionWrites) != 1 {
		t.Errorf("session writes = %d, want the state reconciled once", len(instances.sessionWrites))
	}
}

func TestIntegritySweepReRegistersAMissingWebhook(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	instances := newFakeInstanceRepo(instance)
	provider := &fakeProvider{
		GetWebhooksFn: func(context.Context, uw.InstanceRef) ([]uw.WebhookSubscription, error) {
			return nil, nil
		},
	}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.VerifyIntegrity(context.Background()); err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}

	if len(provider.webhookSets) != 1 {
		t.Fatalf("webhook re-registered %d times, want once", len(provider.webhookSets))
	}
	if got, want := provider.webhookSets[0].URL, uw.WebhookURLFor(testWebhookBase, "dtok"); got != want {
		t.Errorf("re-registered %q, want %q", got, want)
	}
	if instances.webhookStamps != 1 {
		t.Error("a successful re-registration must be stamped")
	}
}

func TestIntegritySweepLeavesAHealthyWebhookAlone(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	instances := newFakeInstanceRepo(instance)
	provider := &fakeProvider{
		GetWebhooksFn: func(context.Context, uw.InstanceRef) ([]uw.WebhookSubscription, error) {
			return []uw.WebhookSubscription{{
				URL: uw.WebhookURLFor(testWebhookBase, "dtok"), Enabled: true,
				Events: uw.SubscribedEvents(),
			}}, nil
		},
	}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.VerifyIntegrity(context.Background()); err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if len(provider.webhookSets) != 0 {
		t.Error("a correctly registered webhook must not be rewritten")
	}
}

func TestIntegritySweepResubscribesWhenEventsAreMissing(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	instances := newFakeInstanceRepo(instance)
	provider := &fakeProvider{
		GetWebhooksFn: func(context.Context, uw.InstanceRef) ([]uw.WebhookSubscription, error) {
			return []uw.WebhookSubscription{{
				URL: uw.WebhookURLFor(testWebhookBase, "dtok"), Enabled: true,
				Events: []string{"messages", "messages_update", "connection"},
			}}, nil
		},
	}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.VerifyIntegrity(context.Background()); err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if len(provider.webhookSets) != 1 {
		t.Fatal("a webhook missing events we consume must be re-registered")
	}
	if missing := missingEvents(provider.webhookSets[0].Events, uw.SubscribedEvents()); len(missing) > 0 {
		t.Errorf("re-registered without %v", missing)
	}
}

func TestWebhookEventComparisonIgnoresCasing(t *testing.T) {
	upper := make([]string, 0, len(uw.SubscribedEvents()))
	for _, e := range uw.SubscribedEvents() {
		upper = append(upper, strings.ToUpper(e))
	}
	if missing := missingEvents(upper, uw.SubscribedEvents()); len(missing) > 0 {
		t.Errorf("case difference reported as missing: %v", missing)
	}
}

func TestIntegritySweepTreatsADisabledWebhookAsMissing(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	instances := newFakeInstanceRepo(instance)
	provider := &fakeProvider{
		GetWebhooksFn: func(context.Context, uw.InstanceRef) ([]uw.WebhookSubscription, error) {
			return []uw.WebhookSubscription{{
				URL: uw.WebhookURLFor(testWebhookBase, "dtok"), Enabled: false,
			}}, nil
		},
	}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.VerifyIntegrity(context.Background()); err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if len(provider.webhookSets) != 1 {
		t.Error("a disabled webhook delivers nothing and must be re-registered")
	}
}

func TestBackstopDoesNotDisconnectOnATransientFailure(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok",
	}
	instances := newFakeInstanceRepo(instance)
	instances.ListForHealthCheckFn = func(time.Time) ([]*uw.Instance, error) {
		return []*uw.Instance{instance}, nil
	}
	provider := &fakeProvider{
		StatusFn: func(context.Context, uw.InstanceRef) (*uw.Session, error) {
			return nil, &uw.ProviderError{HTTPStatus: 502, Message: "bad gateway"}
		},
	}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if instance.Status != uw.StatusConnected {
		t.Errorf("status = %q; a 502 from the host says nothing about the session", instance.Status)
	}
	if len(instances.statusWrites) != 0 {
		t.Errorf("no status may be written on a transient failure, got %v", instances.statusWrites)
	}
}

func TestSweepsIsolatePerInstanceFailures(t *testing.T) {
	broken := &uw.Instance{ID: "broken", WorkspaceID: "ws-1", ServerID: "srv-missing",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "d1"}
	healthy := &uw.Instance{ID: "healthy", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "d2"}

	instances := newFakeInstanceRepo(broken, healthy)
	instances.ListConnectedFn = func() ([]*uw.Instance, error) {
		return []*uw.Instance{broken, healthy}, nil
	}
	provider := &fakeProvider{}

	uc := NewCheckInstanceHealthUseCase(
		instances, newFakeServerRepo(healthyServer("srv-a", 10, 1)), provider, testWebhookBase)
	if err := uc.VerifyIntegrity(context.Background()); err != nil {
		t.Fatalf("an unresolvable host must not abort the sweep: %v", err)
	}
	if len(provider.webhookSets) != 1 {
		t.Errorf("the healthy instance was not reached: %d registrations", len(provider.webhookSets))
	}
}
