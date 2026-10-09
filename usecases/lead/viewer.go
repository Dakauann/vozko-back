package lead_usecase

import (
	"fmt"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/workspace"
)

type DefinitionSource interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type viewers struct {
	permissions Permissions
	definitions DefinitionSource
}

func (s viewers) allowed(a Actor, action workspace.Action) bool {
	return strings.TrimSpace(a.UserID) != "" &&
		s.permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID, string(workspace.ResourceLeads), string(action), a.IsAdmin)
}

func (s viewers) permit(a Actor, actions ...workspace.Action) error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	for _, action := range actions {
		if !s.allowed(a, action) {
			return lead.ErrLeadForbidden
		}
	}
	return nil
}

func (s viewers) authorize(a Actor, actions ...workspace.Action) (lead.Viewer, error) {
	if err := s.permit(a, actions...); err != nil {
		return lead.Viewer{}, err
	}
	return s.of(a)
}

func (s viewers) of(a Actor) (lead.Viewer, error) {
	defs, err := s.definitions.ListByObject(a.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return lead.Viewer{}, fmt.Errorf("lead fields of workspace %s: %w", a.WorkspaceID, err)
	}
	return lead.Viewer{
		ReadsLeads:     s.allowed(a, workspace.ActionRead),
		ReadsAddresses: s.allowed(a, workspace.ActionReadAddresses),
		Fields:         customfield.Viewer{ReadsSensitive: s.allowed(a, workspace.ActionReadSensitive)},
		Definitions:    defs,
	}, nil
}
