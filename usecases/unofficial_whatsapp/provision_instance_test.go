package unofficial_whatsapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

const testWebhookBase = "https://api.example.com"

// Provisioning is the only flow in this channel that can fail with side effects
// on a remote system. Every test here is about a partial failure leaving either
// nothing or something an operator can retry — never an orphan holding a slot.

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

	// The delivery token is the channel's only authenticity control, so it must
	// exist and its digest must match before any event can be resolved.
	if instance.DeliveryToken == "" {
		t.Fatal("no delivery token was minted")
	}
	if instance.DeliveryTokenHash != uw.HashDeliveryToken(instance.DeliveryToken) {
		t.Error("the stored digest does not match the token; the webhook would never resolve")
	}

	// The host's built-in chatbot must be switched off, or two AI brains answer
	// the same customer and neither knows about the other.
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
	// Excluding API-sent messages would cost the delivery-status track and
	// every message an operator types on their own phone.
	if len(sub.ExcludeMessages) != 0 {
		t.Errorf("no exclusion filter may be registered, got %v", sub.ExcludeMessages)
	}
	if instances.webhookStamps != 1 {
		t.Error("a successful registration must be stamped")
	}
}

// The host's console is a shared operational surface. Putting a phone number or
// a company name in the instance name would leak a tenant's identity to anyone
// with access to it.
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
	// The tracing metadata IS how an orphan is matched back to a tenant, so it
	// must be present even though the name is opaque.
	if provider.created[0].WorkspaceID != "ws-1" || provider.created[0].OurInstanceID == "" {
		t.Errorf("tracing metadata missing: %+v", provider.created[0])
	}
}

// A slot claimed for an attempt that failed must go back, or every failure
// permanently shrinks the host's usable capacity.
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

// If the host created an instance we could not persist, that instance is
// unaddressable and would hold a slot forever. It must be deleted.
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

// Webhook registration failing does NOT throw away a working instance: the
// credentials are valid and the operator can retry. But the state must say so,
// because an instance that receives nothing looks exactly like a quiet one.
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

// Placement must skip hosts with no room rather than failing the tenant's click
// at the first full one.
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

// Losing the compare-and-swap for the last slot is a race, not an error: the
// next host must be tried. Two concurrent connects would otherwise both pass a
// read check and one tenant would be told it worked before the host refused.
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

// No capacity anywhere is an honest, actionable answer, not an internal error.
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

// A host that refuses on capacity despite our bookkeeping must still read as a
// capacity problem to the caller, not as an opaque provider failure.
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
