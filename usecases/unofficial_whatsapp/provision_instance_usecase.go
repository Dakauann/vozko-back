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

type ProvisionInstanceUseCase struct {
	servers        uw.ServerRepository
	instances      uw.InstanceRepository
	provider       uw.ProviderAPI
	entitlements   uw.InstanceEntitlementReader
	webhookBaseURL string
}

func (uc *ProvisionInstanceUseCase) SetEntitlements(r uw.InstanceEntitlementReader) {
	uc.entitlements = r
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

type ProvisionInput struct {
	WorkspaceID  string
	DepartmentID *string
	DisplayName  string
}

func (uc *ProvisionInstanceUseCase) Execute(ctx context.Context, in ProvisionInput) (*uw.Instance, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, uw.ErrWorkspaceIDRequired
	}

	if err := uc.checkAllowance(ctx, in.WorkspaceID); err != nil {
		return nil, err
	}

	server, err := uc.claimServer(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}

	instance, err := uc.provision(ctx, server, in)
	if err != nil {
		if releaseErr := uc.servers.ReleaseCapacity(ctx, server.ID); releaseErr != nil {
			log.Printf("[unofficial-whatsapp] release capacity for server %s: %v", server.ID, releaseErr)
		}
		return nil, err
	}
	return instance, nil
}

func (uc *ProvisionInstanceUseCase) checkAllowance(ctx context.Context, workspaceID string) error {
	if uc.entitlements == nil {
		log.Printf("[unofficial-whatsapp] WARNING: provisioning for workspace %s ran with no "+
			"entitlement reader wired; the number allowance was NOT enforced", workspaceID)
		return nil
	}

	allowance, err := uc.entitlements.AllowanceFor(ctx, workspaceID)
	if err != nil {
		log.Printf("[unofficial-whatsapp] entitlement lookup failed for workspace %s: %v", workspaceID, err)
		return uw.ErrEntitlementUnavailable
	}
	return allowance.Enforce()
}

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
	}
	return nil, uw.ErrNoServerCapacity
}

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
		uc.deleteOrphan(ctx, server, created.Token)
		return nil, fmt.Errorf("unofficial whatsapp: persist instance: %w", err)
	}

	uc.configureHost(ctx, server, instance)
	return instance, nil
}

func (uc *ProvisionInstanceUseCase) configureHost(ctx context.Context, server *uw.Server, instance *uw.Instance) {
	ref := uw.RefFor(server, instance)

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

func (uc *ProvisionInstanceUseCase) RegisterWebhook(ctx context.Context, server *uw.Server, instance *uw.Instance) error {
	err := uc.provider.SetWebhook(ctx, uw.RefFor(server, instance), uw.WebhookSubscription{
		URL:             uw.WebhookURLFor(uc.webhookBaseURL, instance.DeliveryToken),
		Enabled:         true,
		Events:          uw.SubscribedEvents(),
		ExcludeMessages: []string{},
	})
	if err != nil {
		return err
	}
	return uc.instances.SetWebhookRegistered(ctx, instance.ID, time.Now().UTC())
}

func (uc *ProvisionInstanceUseCase) deleteOrphan(ctx context.Context, server *uw.Server, token string) {
	err := uc.provider.DeleteInstance(ctx, uw.InstanceRef{BaseURL: server.BaseURL, Token: token})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[unofficial-whatsapp] orphan instance on server %s could not be deleted: %v", server.ID, err)
	}
}

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
