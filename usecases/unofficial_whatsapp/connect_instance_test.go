package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

func connectFixture(status uw.Status) (*fakeInstanceRepo, *fakeServerRepo, *uw.Instance) {
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: status, InstanceToken: "tok", DeliveryToken: "dtok",
	}
	return newFakeInstanceRepo(instance), newFakeServerRepo(healthyServer("srv-a", 10, 1)), instance
}

func TestConnectAttachesTheProvidersOwnDeadline(t *testing.T) {
	t.Run("qr", func(t *testing.T) {
		instances, servers, _ := connectFixture(uw.StatusDisconnected)
		uc := NewConnectInstanceUseCase(instances, servers, &fakeProvider{})

		challenge, err := uc.Connect(context.Background(), ConnectRequest{
			InstanceID: "inst-1", WorkspaceID: "ws-1", Mode: uw.ConnectModeQR,
		})
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		if challenge.QRCode == "" {
			t.Fatal("no QR code returned")
		}
		if challenge.ExpiresAt == nil {
			t.Fatal("a QR code without a deadline leaves the screen unable to refresh it")
		}
		if remaining := time.Until(*challenge.ExpiresAt); remaining > uw.QRCodeTTL+time.Second {
			t.Errorf("QR deadline %v exceeds the provider's %v", remaining, uw.QRCodeTTL)
		}
	})

	t.Run("pairing gets the longer deadline", func(t *testing.T) {
		instances, servers, _ := connectFixture(uw.StatusDisconnected)
		provider := &fakeProvider{
			ConnectFn: func(context.Context, uw.InstanceRef, uw.ConnectInput) (*uw.Session, error) {
				return &uw.Session{State: "connecting", PairCode: "1234-5678"}, nil
			},
		}
		uc := NewConnectInstanceUseCase(instances, servers, provider)

		challenge, err := uc.Connect(context.Background(), ConnectRequest{
			InstanceID: "inst-1", WorkspaceID: "ws-1",
			Mode: uw.ConnectModePairing, Phone: "5511999999999",
		})
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		if challenge.PairCode == "" || challenge.ExpiresAt == nil {
			t.Fatal("pairing needs a code and a deadline")
		}
		if time.Until(*challenge.ExpiresAt) <= uw.QRCodeTTL {
			t.Error("a pairing code has a longer deadline than a QR code")
		}
	})
}

func TestConnectRecordsAwaitingScan(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusDisconnected)
	uc := NewConnectInstanceUseCase(instances, servers, &fakeProvider{})

	if _, err := uc.Connect(context.Background(), ConnectRequest{
		InstanceID: "inst-1", WorkspaceID: "ws-1",
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if instance.Status != uw.StatusAwaitingScan {
		t.Errorf("status = %q, want %q", instance.Status, uw.StatusAwaitingScan)
	}
	if len(instances.sessionWrites) != 1 {
		t.Fatalf("session writes = %d, want 1", len(instances.sessionWrites))
	}
	if got := instances.sessionWrites[0].Status; got == nil || *got != uw.StatusAwaitingScan {
		t.Error("the persisted status must match what the caller was told")
	}
}

func TestConnectRefusesABannedNumber(t *testing.T) {
	instances, servers, _ := connectFixture(uw.StatusBanned)
	provider := &fakeProvider{}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	_, err := uc.Connect(context.Background(), ConnectRequest{InstanceID: "inst-1", WorkspaceID: "ws-1"})
	if !errors.Is(err, uw.ErrStatusTransition) {
		t.Fatalf("err = %v, want a refused transition", err)
	}
	if len(provider.webhookSets) != 0 {
		t.Error("the host must not be called for a request that cannot succeed")
	}
}

func TestConnectEnforcesTenancy(t *testing.T) {
	instances, servers, _ := connectFixture(uw.StatusDisconnected)
	uc := NewConnectInstanceUseCase(instances, servers, &fakeProvider{})

	_, err := uc.Connect(context.Background(), ConnectRequest{
		InstanceID: "inst-1", WorkspaceID: "another-workspace",
	})
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want ErrInstanceNotFound", err)
	}
}

func TestStatusPromotesToConnectedAndCapturesIdentity(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusAwaitingScan)
	uc := NewConnectInstanceUseCase(instances, servers, &fakeProvider{})

	if _, err := uc.Status(context.Background(), "inst-1", "ws-1", uw.Unrestricted()); err != nil {
		t.Fatalf("Status: %v", err)
	}

	if instance.Status != uw.StatusConnected {
		t.Errorf("status = %q, want %q", instance.Status, uw.StatusConnected)
	}
	if instance.JID != "5511999999999@s.whatsapp.net" {
		t.Errorf("jid = %q", instance.JID)
	}
	if instance.PhoneNumber != "5511999999999" {
		t.Errorf("phone = %q, want it derived from the JID", instance.PhoneNumber)
	}
	if instance.ConnectedAt == nil {
		t.Error("the first connection must be stamped")
	}
}

func TestStatusIgnoresAnUnknownProviderState(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	provider := &fakeProvider{
		StatusFn: func(context.Context, uw.InstanceRef) (*uw.Session, error) {
			return &uw.Session{State: "quantum_superposition", Connected: false}, nil
		},
	}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	if _, err := uc.Status(context.Background(), "inst-1", "ws-1", uw.Unrestricted()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if instance.Status != uw.StatusConnected {
		t.Errorf("status = %q; an unknown state must not change it", instance.Status)
	}
	if got := instances.sessionWrites[0].Status; got != nil {
		t.Errorf("no status may be written for an unknown state, got %q", *got)
	}
}

func TestStatusDoesNotBlankAKnownIdentity(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	instance.JID = "5511999999999@s.whatsapp.net"
	instance.PhoneNumber = "5511999999999"
	instance.ProfileName = "Loja ABC"

	provider := &fakeProvider{
		StatusFn: func(context.Context, uw.InstanceRef) (*uw.Session, error) {
			return &uw.Session{State: "disconnected"}, nil
		},
	}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	if _, err := uc.Status(context.Background(), "inst-1", "ws-1", uw.Unrestricted()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if instance.Status != uw.StatusDisconnected {
		t.Errorf("status = %q, want the drop recorded", instance.Status)
	}
	if instance.JID == "" || instance.PhoneNumber == "" || instance.ProfileName == "" {
		t.Error("a disconnected poll must not erase which number this was")
	}
}

func TestStatusRecordsAHostThatNoLongerKnowsTheInstance(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	provider := &fakeProvider{
		StatusFn: func(context.Context, uw.InstanceRef) (*uw.Session, error) {
			return nil, &uw.ProviderError{HTTPStatus: 401, Message: "invalid token"}
		},
	}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	if _, err := uc.Status(context.Background(), "inst-1", "ws-1", uw.Unrestricted()); err != nil {
		t.Fatalf("a lost session is a state, not a request failure: %v", err)
	}
	if instance.Status != uw.StatusDisconnected {
		t.Errorf("status = %q, want %q", instance.Status, uw.StatusDisconnected)
	}
}

func TestDisconnectToleratesAnAlreadyGoneSession(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	provider := &fakeProvider{
		DisconnectFn: func(context.Context, uw.InstanceRef) error {
			return &uw.ProviderError{HTTPStatus: 401}
		},
	}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	if err := uc.Disconnect(context.Background(), "inst-1", "ws-1", uw.Unrestricted()); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if instance.Status != uw.StatusDisconnected {
		t.Errorf("status = %q, want the row to reflect reality", instance.Status)
	}
}

func TestDisconnectSurfacesARealFailure(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	provider := &fakeProvider{
		DisconnectFn: func(context.Context, uw.InstanceRef) error { return errBoom },
	}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	if err := uc.Disconnect(context.Background(), "inst-1", "ws-1", uw.Unrestricted()); err == nil {
		t.Fatal("a transport failure must surface")
	}
	if instance.Status == uw.StatusDisconnected {
		t.Error("the row must not claim a disconnection that did not happen")
	}
}

func TestRotateDeliveryTokenPersistsBeforeReRegistering(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	original := instance.DeliveryToken

	var tokenAtRegistration string
	provider := &fakeProvider{
		SetWebhookFn: func(_ context.Context, _ uw.InstanceRef, sub uw.WebhookSubscription) error {
			tokenAtRegistration = sub.URL
			return nil
		},
	}
	provision := NewProvisionInstanceUseCase(servers, instances, provider, testWebhookBase)
	uc := NewRotateDeliveryTokenUseCase(instances, servers, provision)

	rotated, err := uc.Execute(context.Background(), "inst-1", "ws-1", uw.Unrestricted())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if rotated.DeliveryToken == original {
		t.Fatal("the token did not change")
	}
	if rotated.DeliveryTokenHash != uw.HashDeliveryToken(rotated.DeliveryToken) {
		t.Error("the digest must move with the token or the endpoint stops resolving")
	}
	if tokenAtRegistration != uw.WebhookURLFor(testWebhookBase, rotated.DeliveryToken) {
		t.Errorf("registered %q, want the rotated URL", tokenAtRegistration)
	}
}
