package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type fakeEntitlements struct {
	byKind map[workspace_addon.EntitlementKind]int
	err    error
	calls  int
}

func (f *fakeEntitlements) Execute(string) ([]workspace_addon.WorkspaceEntitlement, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]workspace_addon.WorkspaceEntitlement, 0, len(f.byKind))
	for kind, total := range f.byKind {
		out = append(out, workspace_addon.WorkspaceEntitlement{Kind: kind, Total: total})
	}
	return out, nil
}

type countingInstances struct {
	*fakeInstanceRepo
	count int
	err   error
}

func (c *countingInstances) CountByWorkspace(context.Context, string) (int, error) {
	return c.count, c.err
}

func newAllowanceReader(limit, used int) *InstanceEntitlementReader {
	reader := NewInstanceEntitlementReader(&countingInstances{
		fakeInstanceRepo: newFakeInstanceRepo(),
		count:            used,
	})
	reader.SetSource(&fakeEntitlements{byKind: map[workspace_addon.EntitlementKind]int{
		workspace_addon.EntitlementUnofficialWhatsAppInstances: limit,
	}})
	return reader
}

func TestAllowanceReaderCombinesLimitAndUsage(t *testing.T) {
	allowance, err := newAllowanceReader(5, 2).AllowanceFor(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("AllowanceFor: %v", err)
	}
	if allowance.Limit != 5 || allowance.Used != 2 {
		t.Fatalf("allowance = %+v, want limit 5 used 2", allowance)
	}
	if allowance.Remaining() != 3 {
		t.Errorf("Remaining() = %d, want 3", allowance.Remaining())
	}
}

func TestUnknownKindResolvesToZeroNotAnError(t *testing.T) {
	reader := NewInstanceEntitlementReader(&countingInstances{
		fakeInstanceRepo: newFakeInstanceRepo(),
	})
	reader.SetSource(&fakeEntitlements{byKind: map[workspace_addon.EntitlementKind]int{
		workspace_addon.EntitlementWhatsAppBusinessPhones: 10,
		workspace_addon.EntitlementCallChannels:           4,
	}})

	allowance, err := reader.AllowanceFor(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("AllowanceFor: %v", err)
	}
	if allowance.Limit != 0 {
		t.Errorf("limit = %d, want 0 — another channel's entitlement must not leak into this one",
			allowance.Limit)
	}
}

func TestUnwiredSourceFailsClosed(t *testing.T) {
	reader := NewInstanceEntitlementReader(&countingInstances{
		fakeInstanceRepo: newFakeInstanceRepo(),
	})
	if reader.HasSource() {
		t.Fatal("a freshly built reader must not claim a source")
	}

	allowance, err := reader.AllowanceFor(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("AllowanceFor: %v", err)
	}
	if allowance.CanProvision() {
		t.Error("an unwired entitlement reader permitted provisioning")
	}
}

func provisioningHarness(t *testing.T, limit, used int) (*ProvisionInstanceUseCase, *fakeServerRepo) {
	t.Helper()
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), &fakeProvider{}, testWebhookBase)
	uc.SetEntitlements(newAllowanceReader(limit, used))
	return uc, servers
}

func TestProvisionRefusesWithoutAnAllowance(t *testing.T) {
	uc, servers := provisioningHarness(t, 0, 0)

	_, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, uw.ErrNoInstanceAllowance) {
		t.Fatalf("err = %v, want ErrNoInstanceAllowance", err)
	}
	if servers.claims != 0 {
		t.Errorf("claimed host capacity %d times for a refused provision; a shared host's "+
			"slots must not be taken and handed back by attempts that cannot succeed", servers.claims)
	}
}

func TestProvisionRefusesAtTheLimit(t *testing.T) {
	uc, servers := provisioningHarness(t, 3, 3)

	_, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, uw.ErrInstanceLimitReached) {
		t.Fatalf("err = %v, want ErrInstanceLimitReached", err)
	}
	if servers.claims != 0 {
		t.Errorf("claimed host capacity %d times for a refused provision", servers.claims)
	}
}

func TestProvisionProceedsWithRoom(t *testing.T) {
	uc, _ := provisioningHarness(t, 3, 1)

	instance, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("a workspace with two slots left was refused: %v", err)
	}
	if instance == nil {
		t.Fatal("no instance returned")
	}
}

func TestProvisionFailsClosedWhenEntitlementsAreUnreadable(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), &fakeProvider{}, testWebhookBase)

	reader := NewInstanceEntitlementReader(&countingInstances{fakeInstanceRepo: newFakeInstanceRepo()})
	reader.SetSource(&fakeEntitlements{err: errors.New("billing is down")})
	uc.SetEntitlements(reader)

	_, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, uw.ErrEntitlementUnavailable) {
		t.Fatalf("err = %v, want ErrEntitlementUnavailable", err)
	}
	if servers.claims != 0 {
		t.Errorf("claimed host capacity %d times despite an unreadable entitlement", servers.claims)
	}
}

func TestAllowanceIsCheckedBeforeClaimingCapacity(t *testing.T) {
	uc, servers := provisioningHarness(t, 1, 1)

	_, _ = uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})

	if servers.claims != 0 || servers.releases != 0 {
		t.Errorf("claims=%d releases=%d; a refused provision must not touch host capacity at all",
			servers.claims, servers.releases)
	}
}

func TestUsageCountsEveryInstanceNotOnlyConnectedOnes(t *testing.T) {
	instances := newFakeInstanceRepo(
		&uw.Instance{ID: "i-1", WorkspaceID: "ws-1", Status: uw.StatusConnected},
		&uw.Instance{ID: "i-2", WorkspaceID: "ws-1", Status: uw.StatusDisconnected},
		&uw.Instance{ID: "i-3", WorkspaceID: "ws-1", Status: uw.StatusBanned},
	)

	count, err := instances.CountByWorkspace(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("CountByWorkspace: %v", err)
	}
	if count != 3 {
		t.Errorf("counted %d instances, want 3 — a dead session still holds its slot", count)
	}
}
