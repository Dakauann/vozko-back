// Package unofficial_whatsapp holds the unofficial WhatsApp channel's
// application logic.
//
// Every rule that decides WHETHER something may happen lives in
// domain/unofficial_whatsapp; this layer orchestrates the order things happen
// in and owns the compensation when a step fails halfway.
package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	uw "vozko/domain/unofficial_whatsapp"
)

// ProvisionInstanceUseCase creates a connected-number slot for a workspace.
//
// It is the only place that holds a host's admin token, and the only multi-step
// flow in the channel that can fail with side effects on a remote system. The
// order below is chosen so that every partial failure leaves either nothing or
// something an operator can retry, never an orphan:
//
//	claim capacity → create on host → persist locally → register webhook
//
// Creating on the host before persisting means a crash in between leaves an
// orphan instance, which the health cron's reconciliation finds through the
// admin metadata we write. The reverse order would leave a local row pointing at
// nothing, which nothing can detect.
type ProvisionInstanceUseCase struct {
	servers   uw.ServerRepository
	instances uw.InstanceRepository
	provider  uw.ProviderAPI
	// webhookBaseURL is the public origin the host will POST to. Validated at
	// boot: a wrong scheme produces silence rather than an error.
	webhookBaseURL string
}

func NewProvisionInstanceUseCase(
	servers uw.ServerRepository,
	instances uw.InstanceRepository,
	provider uw.ProviderAPI,
	webhookBaseURL string,
) *ProvisionInstanceUseCase {
	return &ProvisionInstanceUseCase{
		servers:        servers,
		instances:      instances,
		provider:       provider,
		webhookBaseURL: strings.TrimRight(strings.TrimSpace(webhookBaseURL), "/"),
	}
}

// ProvisionInput is one request to add a number to a workspace.
type ProvisionInput struct {
	WorkspaceID  string
	DepartmentID *string
	// DisplayName is what an operator will see in the inbox before the number
	// is known. It is replaced by the real profile once the session connects.
	DisplayName string
}

// Execute provisions an instance and returns it ready to be linked.
func (uc *ProvisionInstanceUseCase) Execute(ctx context.Context, in ProvisionInput) (*uw.Instance, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, uw.ErrWorkspaceIDRequired
	}

	server, err := uc.claimServer(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}

	instance, err := uc.provision(ctx, server, in)
	if err != nil {
		// The slot is only ours while the instance exists. Releasing it here
		// keeps a failed attempt from permanently shrinking a host's capacity.
		if releaseErr := uc.servers.ReleaseCapacity(ctx, server.ID); releaseErr != nil {
			log.Printf("[unofficial-whatsapp] release capacity for server %s: %v", server.ID, releaseErr)
		}
		return nil, err
	}
	return instance, nil
}

// claimServer picks a host and takes a slot on it.
//
// Capacity is claimed with a compare-and-swap rather than a read-then-write:
// two concurrent connects racing for the last free slot would both pass a read
// check, and one tenant would be told their connection succeeded before the host
// refused it.
func (uc *ProvisionInstanceUseCase) claimServer(ctx context.Context, workspaceID string) (*uw.Server, error) {
	candidates, err := uc.servers.ListPlacementCandidates(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: list placement candidates: %w", err)
	}

	for _, server := range candidates {
		if !server.HasCapacity() {
			continue
		}
		claimed, err := uc.servers.ClaimCapacity(ctx, server.ID)
		if err != nil {
			return nil, fmt.Errorf("unofficial whatsapp: claim capacity: %w", err)
		}
		if claimed {
			return server, nil
		}
		// Lost the race for the last slot; try the next host rather than
		// failing the tenant's click.
	}
	return nil, uw.ErrNoServerCapacity
}

// provision creates the instance on the host and persists it.
func (uc *ProvisionInstanceUseCase) provision(
	ctx context.Context,
	server *uw.Server,
	in ProvisionInput,
) (*uw.Instance, error) {
	deliveryToken, err := uw.GenerateDeliveryToken()
	if err != nil {
		return nil, err
	}

	instance := &uw.Instance{
		// Minted HERE, not by the repository's BeforeCreate hook, because it has
		// to travel to the host before the row exists: it is written into the
		// admin metadata, which is the only thing that ties an instance stranded
		// on a host back to a tenant when a crash happens between the remote
		// create and our write. The hook only fills an empty id, so a value set
		// here survives.
		ID:                uuid.New().String(),
		WorkspaceID:       in.WorkspaceID,
		DepartmentID:      in.DepartmentID,
		ServerID:          server.ID,
		Provider:          server.Provider,
		DisplayName:       strings.TrimSpace(in.DisplayName),
		DeliveryToken:     deliveryToken,
		DeliveryTokenHash: uw.HashDeliveryToken(deliveryToken),
		Status:            uw.StatusProvisioning,
	}
	instance.Normalize()
	created, err := uc.provider.CreateInstance(ctx, uw.ServerRef{
		BaseURL:    server.BaseURL,
		AdminToken: server.AdminToken,
	}, uw.CreateInstanceInput{
		Name:          instanceNameFor(in.WorkspaceID, deliveryToken),
		WorkspaceID:   in.WorkspaceID,
		OurInstanceID: instance.ID,
	})
	if err != nil {
		if provErr, ok := uw.AsProviderError(err); ok && provErr.AtCapacity() {
			// The host disagreed with our bookkeeping. Say so honestly rather
			// than surfacing a raw 429, and let the reconciliation fix the
			// counter.
			return nil, fmt.Errorf("%w: %v", uw.ErrNoServerCapacity, provErr)
		}
		return nil, fmt.Errorf("unofficial whatsapp: create instance on host: %w", err)
	}

	instance.ProviderInstanceID = created.ProviderInstanceID
	instance.ProviderName = created.Name
	instance.InstanceToken = created.Token
	if instance.DisplayName == "" {
		instance.DisplayName = created.Name
	}

	if err := uc.instances.Create(ctx, instance); err != nil {
		// The host now holds an instance we cannot address. Delete it rather
		// than leaving it to consume a slot forever; if the delete also fails,
		// the admin metadata makes it findable by the reconciliation job.
		uc.deleteOrphan(ctx, server, created.Token)
		return nil, fmt.Errorf("unofficial whatsapp: persist instance: %w", err)
	}

	uc.configureHost(ctx, server, instance)
	return instance, nil
}

// configureHost applies everything that must be true of a live instance.
//
// Failures here are logged and reflected in the status rather than aborting:
// the instance exists and is addressable, and forcing the operator to start over
// because a follow-up call failed would be worse than showing a repairable
// state.
func (uc *ProvisionInstanceUseCase) configureHost(ctx context.Context, server *uw.Server, instance *uw.Instance) {
	ref := uw.RefFor(server, instance)

	// The host ships its own AI chatbot. Left on, it answers the same customer
	// our agent is answering and neither knows about the other, so this is
	// asserted at provisioning and re-asserted by the health cron.
	if err := uc.provider.DisableBuiltInChatbot(ctx, ref); err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: could not disable the host's built-in chatbot: %v",
			instance.ID, err)
	}

	if err := uc.RegisterWebhook(ctx, server, instance); err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: webhook registration failed: %v", instance.ID, err)
		_ = uc.instances.UpdateStatus(ctx, instance.ID, uw.StatusProvisionFailed,
			"webhook registration failed: "+err.Error())
		instance.Status = uw.StatusProvisionFailed
		instance.StatusReason = err.Error()
		return
	}

	if err := uc.instances.UpdateStatus(ctx, instance.ID, uw.StatusDisconnected, ""); err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: status update failed: %v", instance.ID, err)
		return
	}
	instance.Status = uw.StatusDisconnected
}

// RegisterWebhook points the host at our delivery URL.
//
// Exported because re-registering is also a repair action: a tenant poking the
// host's console can unhook us, and the symptom is indistinguishable from "no
// one messaged today".
func (uc *ProvisionInstanceUseCase) RegisterWebhook(ctx context.Context, server *uw.Server, instance *uw.Instance) error {
	err := uc.provider.SetWebhook(ctx, uw.RefFor(server, instance), uw.WebhookSubscription{
		URL:     uw.WebhookURLFor(uc.webhookBaseURL, instance.DeliveryToken),
		Enabled: true,
		Events:  uw.SubscribedEvents(),
		// Empty, deliberately. See SubscribedEvents: excluding API-sent
		// messages would cost the delivery-status track and every message an
		// operator types on their own phone.
		ExcludeMessages: []string{},
	})
	if err != nil {
		return err
	}
	return uc.instances.SetWebhookRegistered(ctx, instance.ID, time.Now().UTC())
}

// deleteOrphan best-effort removes an instance we created but could not persist.
func (uc *ProvisionInstanceUseCase) deleteOrphan(ctx context.Context, server *uw.Server, token string) {
	err := uc.provider.DeleteInstance(ctx, uw.InstanceRef{BaseURL: server.BaseURL, Token: token})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[unofficial-whatsapp] orphan instance on server %s could not be deleted: %v", server.ID, err)
	}
}

// instanceNameFor builds the label the host shows in its own console.
//
// It carries no customer data: host consoles are shared operational surfaces,
// and putting a phone number or a company name there leaks a tenant's identity
// to anyone with access to the host.
func instanceNameFor(workspaceID, entropy string) string {
	return "vozko-" + shortID(workspaceID) + "-" + shortID(entropy)
}

func shortID(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) > 8 {
		return strings.ToLower(s[:8])
	}
	return strings.ToLower(s)
}
