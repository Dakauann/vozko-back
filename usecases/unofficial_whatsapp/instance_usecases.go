package unofficial_whatsapp

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

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

type GetInstanceUseCase struct {
	instances uw.InstanceRepository
}

func NewGetInstanceUseCase(instances uw.InstanceRepository) *GetInstanceUseCase {
	return &GetInstanceUseCase{instances: instances}
}

func (uc *GetInstanceUseCase) Execute(
	ctx context.Context,
	instanceID, workspaceID string,
	scope uw.DepartmentScope,
) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if workspaceID != "" {
		if err := EnsureVisible(instance, workspaceID, scope); err != nil {
			return nil, err
		}
	}
	return instance, nil
}

type UpdateInstanceConfigUseCase struct {
	instances uw.InstanceRepository
}

func NewUpdateInstanceConfigUseCase(instances uw.InstanceRepository) *UpdateInstanceConfigUseCase {
	return &UpdateInstanceConfigUseCase{instances: instances}
}

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
	EnableAutoMemory     *bool
	HandleGroups         *bool

	DailySendCap    *int
	SendDelayMinMS  *int
	SendDelayMaxMS  *int
	AutoRejectCalls *bool

	Scope uw.DepartmentScope
}

func (uc *UpdateInstanceConfigUseCase) Execute(ctx context.Context, in UpdateInstanceConfigInput) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, in.InstanceID)
	if err != nil {
		return nil, err
	}
	if in.WorkspaceID != "" {
		if err := EnsureVisible(instance, in.WorkspaceID, in.Scope); err != nil {
			return nil, err
		}
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
	applyBool(&instance.EnableAutoMemory, in.EnableAutoMemory)
	applyBool(&instance.HandleGroups, in.HandleGroups)
	applyBool(&instance.AutoRejectCalls, in.AutoRejectCalls)
	applyInt(&instance.DailySendCap, in.DailySendCap)
	applyInt(&instance.SendDelayMinMS, in.SendDelayMinMS)
	applyInt(&instance.SendDelayMaxMS, in.SendDelayMaxMS)

	instance.Normalize()
	if err := instance.Validate(); err != nil {
		return nil, err
	}
	if err := uc.instances.Update(ctx, instance); err != nil {
		return nil, err
	}
	return instance, nil
}

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

func (uc *RotateDeliveryTokenUseCase) Execute(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if workspaceID != "" {
		if err := EnsureVisible(instance, workspaceID, scope); err != nil {
			return nil, err
		}
	}
	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return nil, err
	}

	token, err := uw.GenerateDeliveryToken()
	if err != nil {
		return nil, err
	}

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

func (uc *DeleteInstanceUseCase) Execute(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) error {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if workspaceID != "" {
		if err := EnsureVisible(instance, workspaceID, scope); err != nil {
			return err
		}
	}
	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return err
	}

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

func applyPtr(target **string, value **string) {
	if value != nil {
		*target = *value
	}
}

func EnsureVisible(instance *uw.Instance, workspaceID string, scope uw.DepartmentScope) error {
	if instance == nil || instance.WorkspaceID != workspaceID {
		return uw.ErrInstanceNotFound
	}
	if !scope.AllowsInstance(instance) {
		return uw.ErrInstanceNotFound
	}
	return nil
}

type GetAllowanceUseCase struct {
	entitlements uw.InstanceEntitlementReader
}

func NewGetAllowanceUseCase(entitlements uw.InstanceEntitlementReader) *GetAllowanceUseCase {
	return &GetAllowanceUseCase{entitlements: entitlements}
}

func (uc *GetAllowanceUseCase) Execute(ctx context.Context, workspaceID string) (uw.InstanceAllowance, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return uw.InstanceAllowance{}, uw.ErrWorkspaceIDRequired
	}
	if uc.entitlements == nil {
		return uw.InstanceAllowance{}, uw.ErrEntitlementUnavailable
	}
	return uc.entitlements.AllowanceFor(ctx, workspaceID)
}
