package unofficial_whatsapp

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// The read and configuration use cases.
//
// They are small on purpose: everything that decides whether a change is legal
// is a method on the domain entity, so these orchestrate and enforce tenancy and
// nothing else.

// ListInstancesUseCase lists a workspace's connected numbers.
type ListInstancesUseCase struct {
	instances uw.InstanceRepository
}

func NewListInstancesUseCase(instances uw.InstanceRepository) *ListInstancesUseCase {
	return &ListInstancesUseCase{instances: instances}
}

func (uc *ListInstancesUseCase) Execute(ctx context.Context, in uw.ListInstancesInput) (*shared.PaginatedResult[*uw.Instance], error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return nil, uw.ErrWorkspaceIDRequired
	}
	return uc.instances.ListByWorkspace(ctx, in)
}

// GetInstanceUseCase reads one instance, scoped to its workspace.
type GetInstanceUseCase struct {
	instances uw.InstanceRepository
}

func NewGetInstanceUseCase(instances uw.InstanceRepository) *GetInstanceUseCase {
	return &GetInstanceUseCase{instances: instances}
}

func (uc *GetInstanceUseCase) Execute(ctx context.Context, instanceID, workspaceID string) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if workspaceID != "" && instance.WorkspaceID != workspaceID {
		// Not found rather than forbidden: confirming existence would let a
		// caller enumerate other tenants' instance ids.
		return nil, uw.ErrInstanceNotFound
	}
	return instance, nil
}

// UpdateInstanceConfigUseCase applies operator-editable settings.
type UpdateInstanceConfigUseCase struct {
	instances uw.InstanceRepository
}

func NewUpdateInstanceConfigUseCase(instances uw.InstanceRepository) *UpdateInstanceConfigUseCase {
	return &UpdateInstanceConfigUseCase{instances: instances}
}

// UpdateInstanceConfigInput carries every field an operator may change.
//
// Pointers throughout so "not supplied" and "set to false/zero" are
// distinguishable: a PATCH that omitted a toggle must not silently switch it
// off, which is exactly what a bare bool would do.
type UpdateInstanceConfigInput struct {
	InstanceID  string
	WorkspaceID string

	DisplayName  *string
	DepartmentID **string

	AgentID    **string
	WorkflowID **string
	PipelineID **string

	EnableAgentResponses *bool
	EnableWorkflow       *bool
	EnableAnalysis       *bool
	EnableAutoStaging    *bool
	HandleGroups         *bool

	DailySendCap    *int
	SendDelayMinMS  *int
	SendDelayMaxMS  *int
	AutoRejectCalls *bool
}

func (uc *UpdateInstanceConfigUseCase) Execute(ctx context.Context, in UpdateInstanceConfigInput) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, in.InstanceID)
	if err != nil {
		return nil, err
	}
	if in.WorkspaceID != "" && instance.WorkspaceID != in.WorkspaceID {
		return nil, uw.ErrInstanceNotFound
	}

	applyString(&instance.DisplayName, in.DisplayName)
	applyPtr(&instance.DepartmentID, in.DepartmentID)
	applyPtr(&instance.AgentID, in.AgentID)
	applyPtr(&instance.WorkflowID, in.WorkflowID)
	applyPtr(&instance.PipelineID, in.PipelineID)
	applyBool(&instance.EnableAgentResponses, in.EnableAgentResponses)
	applyBool(&instance.EnableWorkflow, in.EnableWorkflow)
	applyBool(&instance.EnableAnalysis, in.EnableAnalysis)
	applyBool(&instance.EnableAutoStaging, in.EnableAutoStaging)
	applyBool(&instance.HandleGroups, in.HandleGroups)
	applyBool(&instance.AutoRejectCalls, in.AutoRejectCalls)
	applyInt(&instance.DailySendCap, in.DailySendCap)
	applyInt(&instance.SendDelayMinMS, in.SendDelayMinMS)
	applyInt(&instance.SendDelayMaxMS, in.SendDelayMaxMS)

	// Normalize clamps the pacing floor and orders the delay bounds; Validate
	// then rejects anything still outside the rules. Both live on the entity so
	// the HTTP layer cannot disagree with the cron about what is legal.
	instance.Normalize()
	if err := instance.Validate(); err != nil {
		return nil, err
	}
	if err := uc.instances.Update(ctx, instance); err != nil {
		return nil, err
	}
	return instance, nil
}

// RotateDeliveryTokenUseCase mints a new webhook URL and re-registers it.
//
// A one-click action because the delivery token is a bearer credential that
// travels in a URL: leaking through a proxy log or an error reporter is a when,
// not an if, and a rotation that takes a support ticket will not happen.
type RotateDeliveryTokenUseCase struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	provision *ProvisionInstanceUseCase
}

func NewRotateDeliveryTokenUseCase(
	instances uw.InstanceRepository,
	servers uw.ServerRepository,
	provision *ProvisionInstanceUseCase,
) *RotateDeliveryTokenUseCase {
	return &RotateDeliveryTokenUseCase{instances: instances, servers: servers, provision: provision}
}

func (uc *RotateDeliveryTokenUseCase) Execute(ctx context.Context, instanceID, workspaceID string) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if workspaceID != "" && instance.WorkspaceID != workspaceID {
		return nil, uw.ErrInstanceNotFound
	}
	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, err
	}

	token, err := uw.GenerateDeliveryToken()
	if err != nil {
		return nil, err
	}

	// Persist first, then tell the host. The other order would leave a window
	// where the host posts to a URL we no longer resolve, and every event in it
	// is lost — this provider has no replay.
	if err := uc.instances.RotateDeliveryToken(ctx, instance.ID, token, uw.HashDeliveryToken(token)); err != nil {
		return nil, err
	}
	instance.DeliveryToken = token
	instance.DeliveryTokenHash = uw.HashDeliveryToken(token)

	if err := uc.provision.RegisterWebhook(ctx, server, instance); err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: re-register webhook after rotation: %w", err)
	}
	return instance, nil
}

// DeleteInstanceUseCase removes a number from a workspace.
type DeleteInstanceUseCase struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	provider  uw.ProviderAPI
}

func NewDeleteInstanceUseCase(
	instances uw.InstanceRepository,
	servers uw.ServerRepository,
	provider uw.ProviderAPI,
) *DeleteInstanceUseCase {
	return &DeleteInstanceUseCase{instances: instances, servers: servers, provider: provider}
}

func (uc *DeleteInstanceUseCase) Execute(ctx context.Context, instanceID, workspaceID string) error {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if workspaceID != "" && instance.WorkspaceID != workspaceID {
		return uw.ErrInstanceNotFound
	}
	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return err
	}

	// Remove from the host first: a host-side failure after our row is gone
	// leaves an instance nothing can address, holding a capacity slot forever.
	// A failure here is logged rather than fatal, because refusing to delete
	// locally would leave the tenant unable to remove a number they no longer
	// control — the reconciliation job finds the leftover through its admin
	// metadata.
	if err := uc.provider.DeleteInstance(ctx, uw.RefFor(server, instance)); err != nil {
		if provErr, ok := uw.AsProviderError(err); !ok || !provErr.NeedsReconnect() {
			log.Printf("[unofficial-whatsapp] instance %s: host delete failed, removing locally anyway: %v",
				instance.ID, err)
		}
	}

	if err := uc.instances.Delete(ctx, instance.ID); err != nil {
		return err
	}
	if err := uc.servers.ReleaseCapacity(ctx, server.ID); err != nil {
		log.Printf("[unofficial-whatsapp] release capacity for server %s: %v", server.ID, err)
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func applyString(target *string, value *string) {
	if value != nil {
		*target = strings.TrimSpace(*value)
	}
}

func applyBool(target *bool, value *bool) {
	if value != nil {
		*target = *value
	}
}

func applyInt(target *int, value *int) {
	if value != nil {
		*target = *value
	}
}

// applyPtr assigns a nullable field. The double pointer is what distinguishes
// "leave it alone" (nil) from "clear it" (pointer to nil).
func applyPtr(target **string, value **string) {
	if value != nil {
		*target = *value
	}
}
