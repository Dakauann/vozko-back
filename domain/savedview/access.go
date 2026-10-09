package savedview

import (
	"errors"
	"fmt"

	"vozko/domain/conversation"
	"vozko/domain/workspace"
)

type Operation = workspace.Operation

const (
	OperationRead   = workspace.OperationRead
	OperationCreate = workspace.OperationCreate
	OperationUpdate = workspace.OperationUpdate
	OperationDelete = workspace.OperationDelete
)

var ErrForbidden = errors.New("savedview: missing permission for the views of this object")

type Actor = conversation.Viewer

func RequiredPermission(object ObjectType, op Operation) (workspace.PermissionEntry, error) {
	if !object.Valid() {
		return workspace.PermissionEntry{}, ErrInvalidObject
	}
	action, known := workspace.CRUDAction(op)
	if !known {
		return workspace.PermissionEntry{}, fmt.Errorf("%w: unknown operation %q", ErrForbidden, op)
	}
	if object == ObjectLead {
		return workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}, nil
	}
	return workspace.PermissionEntry{Resource: workspace.ResourceConversations, Action: action}, nil
}
