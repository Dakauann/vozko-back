package businessphone_usecase

import (
	"errors"
	"testing"

	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type failingEntitlements struct{ err error }

func (f failingEntitlements) Execute(string) ([]workspace_addon.WorkspaceEntitlement, error) {
	return nil, f.err
}

type noPhoneEntitlements struct{}

func (noPhoneEntitlements) Execute(string) ([]workspace_addon.WorkspaceEntitlement, error) {
	return nil, nil
}

func TestPhoneProvisioningGate_CountsOfficialPhonesOfEveryProvider(t *testing.T) {
	reader := &fakeOwnerReader{active: 0, official: 2}
	gate := NewPhoneProvisioningGate(&fakeEntitlements{total: 2}, reader)

	ok, err := gate.CanProvisionPhone("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("two official phones against an entitlement of two must refuse a third, whatever the provider")
	}
}

func TestPhoneProvisioningGate_AllowsUnderEntitlement(t *testing.T) {
	reader := &fakeOwnerReader{active: 1, official: 1}
	gate := NewPhoneProvisioningGate(&fakeEntitlements{total: 2}, reader)

	ok, err := gate.CanProvisionPhone("ws-1")
	if err != nil || !ok {
		t.Fatalf("expected allowed, got ok=%v err=%v", ok, err)
	}
}

func TestPhoneProvisioningGate_NoEntitlementRefuses(t *testing.T) {
	gate := NewPhoneProvisioningGate(noPhoneEntitlements{}, &fakeOwnerReader{})

	ok, err := gate.CanProvisionPhone("ws-1")
	if err != nil || ok {
		t.Fatalf("expected refusal without an entitlement, got ok=%v err=%v", ok, err)
	}
}

func TestPhoneProvisioningGate_EntitlementErrorPropagates(t *testing.T) {
	boom := errors.New("subscription not found")
	gate := NewPhoneProvisioningGate(failingEntitlements{err: boom}, &fakeOwnerReader{})

	ok, err := gate.CanProvisionPhone("ws-1")
	if ok || !errors.Is(err, boom) {
		t.Fatalf("expected fail-closed with the entitlement error, got ok=%v err=%v", ok, err)
	}
}
