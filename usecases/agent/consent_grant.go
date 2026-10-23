package agent_usecase

import (
	"context"
	"fmt"

	"vozko/domain/agent"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	tools_usecase "vozko/usecases/tools"
)

type ToolBindingGuarded interface {
	GuardToolBindings(permissions workspace.PermissionChecker)
}

type toolBindingGuard struct {
	permissions workspace.PermissionChecker
}

func (g *toolBindingGuard) GuardToolBindings(permissions workspace.PermissionChecker) {
	g.permissions = permissions
}

func (g *toolBindingGuard) refuseUngrantedBindings(ctx context.Context, registry tools.Service, workspaceID string, by workspace.Requester, stored, proposed []agent.ToolBinding) error {
	if agent.BindingChanged(stored, proposed, tools_usecase.RecordConsentToolName) {
		if err := workspace.RequireConsentGrant(g.permissions, workspaceID, by); err != nil {
			return err
		}
	}
	sheets := agent.BindingsAdded(stored, proposed, tools_usecase.AddSheetRowToolName)
	if len(sheets) > 0 {
		if err := workspace.RequireSheetBinding(g.permissions, workspaceID, by); err != nil {
			return err
		}
	}
	return validateBindingTargets(ctx, registry, workspaceID, sheets)
}

func validateBindingTargets(ctx context.Context, registry tools.Service, workspaceID string, bindings []agent.ToolBinding) error {
	for _, binding := range bindings {
		handler, ok := registry.Handler(binding.Name)
		if !ok {
			return fmt.Errorf("%w: %s", agent.ErrAgentInternalToolInvalid, binding.Name)
		}
		validator, ok := handler.(tools.BindingValidator)
		if !ok {
			continue
		}
		if err := validator.ValidateBinding(ctx, workspaceID, binding.Config); err != nil {
			return fmt.Errorf("%w: %s config invalid: %v", agent.ErrAgentInternalToolInvalid, binding.Name, err)
		}
	}
	return nil
}
