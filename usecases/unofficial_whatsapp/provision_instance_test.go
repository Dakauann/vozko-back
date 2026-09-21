package unofficial_whatsapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

const testWebhookBase = "https://api.example.com"

func TestProvisionHappyPath(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	instances := newFakeInstanceRepo()
	provider := &fakeProvider{}

	uc := NewProvisionInstanceUseCase(servers, instances, provider, testWebhookBase)
	instance, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if instance.ServerID != "srv-a" {
		t.Errorf("placed on %q", instance.ServerID)
	}
	if instance.ProviderInstanceID == "" || instance.InstanceToken == "" {
		t.Error("the instance must be addressable after provisioning")
	}
	if instance.Status != uw.StatusDisconnected {
		t.Errorf("status = %q, want %q (provisioned, awaiting a link)", instance.Status, uw.StatusDisconnected)
	}

	if instance.DeliveryToken == "" {
		t.Fatal("no delivery token was minted")
	}
	if instance.DeliveryTokenHash != uw.HashDeliveryToken(instance.DeliveryToken) {
		t.Error("the stored digest does not match the token; the webhook would never resolve")
	}

	if provider.chatbotDisabled != 1 {
		t.Errorf("the host's chatbot was disabled %d times, want once", provider.chatbotDisabled)
	}

	if len(provider.webhookSets) != 1 {
		t.Fatalf("webhook registered %d times, want once", len(provider.webhookSets))
	}
	sub := provider.webhookSets[0]
	if !strings.HasSuffix(sub.URL, instance.DeliveryToken) {
		t.Errorf("registered URL %q does not carry the delivery token", sub.URL)
	}
	if len(sub.ExcludeMessages) != 0 {
		t.Errorf("no exclusion filter may be registered, got %v", sub.ExcludeMessages)
	}
	if instances.webhookStamps != 1 {
		t.Error("a successful registration must be stamped")
	}
}

func TestProvisionDoesNotLeakTenantIdentityToTheHost(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	provider := &fakeProvider{}

	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), provider, testWebhookBase)
	_, err := uc.Execute(context.Background(), ProvisionInput{
		WorkspaceID: "ws-1",
		DisplayName: "Loja do João — Cobrança",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	name := provider.created[0].Name
	if strings.Contains(strings.ToLower(name), "loja") || strings.Contains(name, "João") {
		t.Errorf("instance name %q leaks the tenant's own naming to the host console", name)
	}
	if provider.created[0].WorkspaceID != "ws-1" || provider.created[0].OurInstanceID == "" {
		t.Errorf("tracing metadata missing: %+v", provider.created[0])
	}
}

func TestProvisionReleasesCapacityWhenTheHostRefuses(t *testing.T) {
	server := healthyServer("srv-a", 10, 0)
	servers := newFakeServerRepo(server)
	provider := &fakeProvider{
		CreateInstanceFn: func(context.Context, uw.ServerRef, uw.CreateInstanceInput) (*uw.CreatedInstance, error) {
			return nil, errBoom
		},
	}

	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), provider, testWebhookBase)
	if _, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"}); err == nil {
		t.Fatal("a host failure must surface")
	}

	if servers.releases != 1 {
		t.Errorf("capacity released %d times, want once", servers.releases)
	}
	if server.InUse != 0 {
		t.Errorf("server InUse = %d after a failed attempt, want 0", server.InUse)
	}
}

func TestProvisionDeletesTheOrphanWhenPersistenceFails(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	instances := newFakeInstanceRepo()
	instances.CreateFn = func(context.Context, *uw.Instance) error { return errBoom }
	provider := &fakeProvider{}

	uc := NewProvisionInstanceUseCase(servers, instances, provider, testWebhookBase)
	if _, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"}); err == nil {
		t.Fatal("a persistence failure must surface")
	}

	if len(provider.deletedTokens) != 1 || provider.deletedTokens[0] != "instance-token" {
		t.Errorf("the orphan was not deleted from the host: %v", provider.deletedTokens)
	}
	if servers.releases != 1 {
		t.Error("the capacity slot must be released too")
	}
}

func TestProvisionKeepsTheInstanceWhenWebhookRegistrationFails(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	instances := newFakeInstanceRepo()
	provider := &fakeProvider{
		SetWebhookFn: func(context.Context, uw.InstanceRef, uw.WebhookSubscription) error {
			return errBoom
		},
	}

	uc := NewProvisionInstanceUseCase(servers, instances, provider, testWebhookBase)
	instance, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("the instance must survive a webhook failure: %v", err)
	}

	if instance.Status != uw.StatusProvisionFailed {
		t.Errorf("status = %q, want %q so the UI can offer a repair",
			instance.Status, uw.StatusProvisionFailed)
	}
	if instance.StatusReason == "" {
		t.Error("the failure reason must be recorded; otherwise nobody knows what to retry")
	}
	if len(provider.deletedTokens) != 0 {
		t.Error("a working instance must not be thrown away over a retryable registration failure")
	}
}

func TestProvisionSkipsFullHosts(t *testing.T) {
	full := healthyServer("srv-full", 1, 1)
	free := healthyServer("srv-free", 10, 0)
	servers := newFakeServerRepo(full, free)
	servers.ListFn = func(context.Context, string) ([]*uw.Server, error) {
		return []*uw.Server{full, free}, nil
	}

	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), &fakeProvider{}, testWebhookBase)
	instance, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if instance.ServerID != "srv-free" {
		t.Errorf("placed on %q, want the host with room", instance.ServerID)
	}
}

func TestProvisionRetriesAnotherHostAfterLosingTheCapacityRace(t *testing.T) {
	contended := healthyServer("srv-contended", 1, 0)
	backup := healthyServer("srv-backup", 10, 0)
	servers := newFakeServerRepo(contended, backup)
	servers.ListFn = func(context.Context, string) ([]*uw.Server, error) {
		return []*uw.Server{contended, backup}, nil
	}
	servers.ClaimFn = func(_ context.Context, serverID string) (bool, error) {
		return serverID != "srv-contended", nil
	}

	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), &fakeProvider{}, testWebhookBase)
	instance, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if instance.ServerID != "srv-backup" {
		t.Errorf("placed on %q after losing a race, want the backup host", instance.ServerID)
	}
	if servers.claims != 2 {
		t.Errorf("claim attempts = %d, want 2 (one lost, one won)", servers.claims)
	}
}

func TestProvisionWithNoCapacityAnywhere(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-full", 1, 1))
	provider := &fakeProvider{}

	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), provider, testWebhookBase)
	_, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, uw.ErrNoServerCapacity) {
		t.Fatalf("err = %v, want ErrNoServerCapacity", err)
	}
	if len(provider.created) != 0 {
		t.Error("no host may be called when there is nowhere to place the instance")
	}
}

func TestProvisionTranslatesAHostCapacityRefusal(t *testing.T) {
	servers := newFakeServerRepo(healthyServer("srv-a", 10, 0))
	provider := &fakeProvider{
		CreateInstanceFn: func(context.Context, uw.ServerRef, uw.CreateInstanceInput) (*uw.CreatedInstance, error) {
			return nil, &uw.ProviderError{HTTPStatus: 429, Message: "instance limit reached"}
		},
	}

	uc := NewProvisionInstanceUseCase(servers, newFakeInstanceRepo(), provider, testWebhookBase)
	_, err := uc.Execute(context.Background(), ProvisionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, uw.ErrNoServerCapacity) {
		t.Fatalf("err = %v, want it to read as a capacity problem", err)
	}
}

func TestProvisionRequiresAWorkspace(t *testing.T) {
	uc := NewProvisionInstanceUseCase(
		newFakeServerRepo(healthyServer("srv-a", 10, 0)), newFakeInstanceRepo(), &fakeProvider{}, testWebhookBase)
	if _, err := uc.Execute(context.Background(), ProvisionInput{}); !errors.Is(err, uw.ErrWorkspaceIDRequired) {
		t.Fatalf("err = %v, want ErrWorkspaceIDRequired", err)
	}
}
