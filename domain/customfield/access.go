package customfield

import (
	"errors"
	"fmt"

	"vozko/domain/workspace"
)

type Operation = workspace.Operation

const (
	OperationRead   = workspace.OperationRead
	OperationCreate = workspace.OperationCreate
	OperationUpdate = workspace.OperationUpdate
	OperationDelete = workspace.OperationDelete
)

var ErrForbidden = errors.New("customfield: missing permission for the fields of this object")

func RequiredPermissions(object ObjectType, op Operation, sensitive bool) ([]workspace.PermissionEntry, error) {
	if !object.Valid() {
		return nil, ErrInvalidObjectType
	}
	action, known := workspace.CRUDAction(op)
	if !known {
		return nil, fmt.Errorf("%w: unknown operation %q", ErrForbidden, op)
	}
	if object != ObjectLead {
		return []workspace.PermissionEntry{{Resource: workspace.ResourceConversations, Action: action}}, nil
	}
	if !op.Writes() {
		return []workspace.PermissionEntry{{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}}, nil
	}
	need := []workspace.PermissionEntry{{Resource: workspace.ResourceLeads, Action: workspace.ActionConfigure}}
	if sensitive {
		need = append(need, workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionReadSensitive})
	}
	return need, nil
}

func TouchesSensitiveData(before, after *Definition) bool {
	return (before != nil && before.Sensitive) || (after != nil && after.Sensitive)
}

func CreationTouchesSensitiveData(retired []*Definition, created *Definition) bool {
	for _, r := range retired {
		if TouchesSensitiveData(r, nil) {
			return true
		}
	}
	return TouchesSensitiveData(nil, created)
}
