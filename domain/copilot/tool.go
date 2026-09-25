package copilot

import (
	"context"

	"vozko/domain/tools"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type Context struct {
	WorkspaceID string
	UserID      string
	Role        workspace.Role
	SystemAdmin bool
	Departments *wd.DepartmentFilter
	View        View
	Datasets    *DatasetStore
}

type Meta struct {
	Mutating bool
	Resource workspace.Resource
	Action   workspace.Action
}

type Status string

const (
	StatusOK     Status = "ok"
	StatusDenied Status = "denied"
	StatusError  Status = "error"
)

type Result struct {
	Status  Status
	Data    interface{}
	Message string
	Chart   *Chart
}

type PendingAction struct {
	ID       string                 `json:"id"`
	ToolName string                 `json:"toolName"`
	Args     map[string]interface{} `json:"args"`
	Summary  string                 `json:"summary"`
}

type Tool interface {
	Definition() tools.Definition
	Meta() Meta
	Execute(ctx context.Context, cc Context, args map[string]interface{}) Result
}
