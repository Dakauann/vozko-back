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

// Execute reads one instance, scoped to its workspace AND the caller's
// departments.
//
// The scope argument is required rather than optional because this endpoint is
// reachable by URL: a member who cannot see a number in the list could
// otherwise open it by guessing its id, and read its configuration, its
// restriction state and its department.
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
		// Tenancy and department in one check, both answering not-found:
		// confirming existence would let a caller enumerate other tenants'
		// instance ids, or discover which numbers another department runs.
		if err := EnsureVisible(instance, workspaceID, scope); err != nil {
			return nil, err
		}
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
	EnableAutoMemory     *bool
	HandleGroups         *bool

	DailySendCap    *int
	SendDelayMinMS  *int
	SendDelayMaxMS  *int
	AutoRejectCalls *bool

	// Scope limits which numbers this caller may edit — including which
	// department the number itself belongs to. Reassigning a number to another
	// department is how a member could otherwise move it out of their own reach,
	// or pull another team's number into it.
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

// GetAllowanceUseCase reports how many numbers a workspace may connect.
//
// Its own endpoint rather than a field on the instance list, for two reasons:
// the connect screen needs it before any list is rendered, and folding it into
// a paginated list would make "how many may I have" a property of page 1.
//
// It is the SAME reader the provisioning gate consults, so the number an
// operator sees and the number that refuses them cannot disagree — which is the
// whole failure mode a separate read-side calculation would introduce.
// EnsureVisible refuses an instance the caller's departments do not cover.
//
// Shared by every endpoint that acts on one number — read, edit, connect,
// reset, rotate, delete, start a conversation — because they all need the same
// answer and a per-handler check is a check that will be forgotten on the
// seventh one.
//
// Returns the not-found error rather than a distinct forbidden one: whether a
// number exists inside a department you are not in is itself information.
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
