package savedview

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

func TestRequiredPermission_LeadViewsFollowTheLeadReaders(t *testing.T) {
	leadsRead := workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
	cases := []struct {
		object ObjectType
		op     Operation
		want   workspace.PermissionEntry
	}{
		{ObjectLead, OperationRead, leadsRead},
		{ObjectLead, OperationCreate, leadsRead},
		{ObjectLead, OperationUpdate, leadsRead},
		{ObjectLead, OperationDelete, leadsRead},
		{ObjectConversation, OperationRead, workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: workspace.ActionRead}},
		{ObjectConversation, OperationCreate, workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: workspace.ActionCreate}},
		{ObjectOpportunity, OperationUpdate, workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: workspace.ActionUpdate}},
		{ObjectOpportunity, OperationDelete, workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: workspace.ActionDelete}},
	}
	for _, tc := range cases {
		got, err := RequiredPermission(tc.object, tc.op)
		if err != nil || got != tc.want {
			t.Errorf("RequiredPermission(%s, %s) = %v, %v; want %v", tc.object, tc.op, got, err, tc.want)
		}
	}
	if _, err := RequiredPermission("board", OperationRead); !errors.Is(err, ErrInvalidObject) {
		t.Fatalf("an unknown object = %v, want ErrInvalidObject", err)
	}
	if _, err := RequiredPermission(ObjectLead, "share"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an unknown operation = %v, want ErrForbidden", err)
	}
}
