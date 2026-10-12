package copilottools

import (
	"time"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type Clock func() time.Time

type DepartmentLookup interface {
	Get(workspaceID, id string) (*wd.Department, error)
}

func ChatReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

func Keep(cc copilot.Context, d *copilot.Dataset) *copilot.Dataset {
	if cc.Datasets == nil {
		return d
	}
	return cc.Datasets.Put(d)
}
