package unofficial_whatsapp

import (
	"context"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// The provider pushes a `connection` event on every session-state change, so
// polling for session state is a BACKSTOP, not the primary signal. These tests
// pin that division of labour: the cheap sweep must not duplicate the webhook,
// and the expensive one must cover exactly what no event can report.

// The backstop asks the repository for instances that have gone quiet, and the
// staleness window is what keeps it from re-polling everything every run.
func TestSessionBackstopOnlyProbesStaleInstances(t *testing.T) {
	instances := newFakeInstanceRepo()
	var askedFor time.Time
	instances.ListForHealthCheckFn = func(before time.Time) ([]*uw.Instance, error) {
		askedFor = before
		// Nothing is stale: the webhook has spoken for every instance.
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
	// A window in the seconds would defeat the point: the webhook needs room to
	// report first, or the backstop is just a second, slower copy of it.
	if window := time.Since(askedFor); window < time.Minute {
		t.Errorf("staleness window is %v; too tight to let the connection webhook report first", window)
	}
}

// The cheap sweep must stay cheap: one call per stale instance, and none of the
// three integrity probes, which are an order of magnitude more traffic.
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

// A webhook silently removed from the host console cannot announce itself: the
// announcement would have to travel through the thing that was removed. This is
// the failure the integrity sweep exists for, and it must self-heal.
func TestIntegritySweepReRegistersAMissingWebhook(t *testing.T) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	instances := newFakeInstanceRepo(instance)
	provider := &fakeProvider{
		GetWebhooksFn: func(context.Context, uw.InstanceRef) ([]uw.WebhookSubscription, error) {
			// The host has no webhook at all: someone unhooked us.
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

// A webhook that is present and enabled must be left alone: rewriting it on
// every hourly pass would be pointless traffic against every host.
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

// A webhook registered but DISABLED delivers nothing while looking configured.
// It has to be treated as missing, or the inbox stays quiet with a healthy-
// looking config behind it.
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

// A transient host failure is not evidence about the session. Marking it
// disconnected would close every composer on the channel each time the host had
// a bad minute.
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

// One tenant's broken instance must not blind us to every other: this job is
// the backstop, and aborting the loop would take the backstop away.
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
	// The healthy instance was still probed despite the broken one coming first.
	if len(provider.webhookSets) != 1 {
		t.Errorf("the healthy instance was not reached: %d registrations", len(provider.webhookSets))
	}
}
