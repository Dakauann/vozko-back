package tooltest

import (
	"vozko/domain/copilot"
	wd "vozko/domain/workspace/workspace_department"
)

const KnownDepartment = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"

var (
	AccessCtx = copilot.Context{WorkspaceID: "ws-1", UserID: "user-1"}
	AdContext = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1"}
)

func OwnerOn(view copilot.View) copilot.Context {
	return copilot.Context{
		WorkspaceID: "ws-1",
		Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true},
		View:        view,
		Datasets:    copilot.NewDatasetStore(),
	}
}

func MemberOf(departments ...string) copilot.Context {
	cc := OwnerOn(copilot.View{})
	cc.Departments = &wd.DepartmentFilter{DepartmentIDs: departments, WorkspaceHasDepartments: true}
	return cc
}

func Member() copilot.Context {
	cc := MemberOf(KnownDepartment)
	cc.UserID = "u-1"
	return cc
}
