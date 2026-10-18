package agent_usecase

import (
	"vozko/domain/agent"
	"vozko/domain/workspace"
	tools_usecase "vozko/usecases/tools"
)

type ConsentGrantGuarded interface {
	GuardConsentGrants(permissions workspace.PermissionChecker)
}

type consentGrantGuard struct {
	permissions workspace.PermissionChecker
}

func (g *consentGrantGuard) GuardConsentGrants(permissions workspace.PermissionChecker) {
	g.permissions = permissions
}

func (g *consentGrantGuard) refuseUngrantedConsentSkill(workspaceID string, by workspace.Requester, stored, proposed []agent.ToolBinding) error {
	if !agent.BindingChanged(stored, proposed, tools_usecase.RecordConsentToolName) {
		return nil
	}
	return workspace.RequireConsentGrant(g.permissions, workspaceID, by)
}
