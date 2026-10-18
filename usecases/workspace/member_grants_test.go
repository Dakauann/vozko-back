package workspace_usecase

import (
	"testing"

	"vozko/domain/workspace"
)

func TestARolesRetiredResourcesNeverBecomeGrants(t *testing.T) {
	grants := memberGrants("m1", []workspace.PermissionEntry{
		{Resource: workspace.ResourceConversations, Action: workspace.ActionRead},
		{Resource: workspace.Resource("campaigns"), Action: workspace.ActionRead},
		{Resource: workspace.Resource("dialer"), Action: workspace.ActionRead},
		{Resource: workspace.Resource("analysis"), Action: workspace.ActionRead},
	})
	if len(grants) != 1 || grants[0].Resource != workspace.ResourceConversations || grants[0].MemberID != "m1" || grants[0].ID == "" {
		t.Fatalf("grants = %+v, want only the conversations grant for the member", grants)
	}
}
