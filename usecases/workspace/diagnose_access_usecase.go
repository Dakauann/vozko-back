package workspace_usecase

import (
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type DepartmentDirectory interface {
	wd.MemberDepartments
	ListDepartmentsByIDs(ids []string) ([]wd.Department, error)
}

type diagnoseAccessUseCase struct {
	repo        workspace.Repository
	permissions workspace.GetMemberPermissionsUseCase
	departments DepartmentDirectory
}

func NewDiagnoseAccessUseCase(repo workspace.Repository, permissions workspace.GetMemberPermissionsUseCase, departments DepartmentDirectory) workspace.DiagnoseAccessUseCase {
	return &diagnoseAccessUseCase{repo: repo, permissions: permissions, departments: departments}
}

func (uc *diagnoseAccessUseCase) Execute(in workspace.DiagnoseAccessInput) (*workspace.AccessDiagnosis, error) {
	feature, ok := workspace.FeatureByKey(in.Feature)
	if !ok {
		return nil, workspace.ErrUnknownFeature
	}
	if in.MemberUserID == "" {
		in.MemberUserID = in.ActorID
	}
	granted, err := uc.permissions.Execute(in.ActorID, in.WorkspaceID, in.MemberUserID, in.CallerRole)
	if err != nil {
		return nil, err
	}
	member, err := uc.repo.GetMember(in.WorkspaceID, in.MemberUserID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, workspace.ErrMemberNotFound
	}
	grants := workspace.NewGrants(member.Role, false, workspace.EntriesOf(granted))
	scope, err := uc.memberScope(in, grants)
	if err != nil {
		return nil, err
	}
	return &workspace.AccessDiagnosis{Member: member, FeatureDiagnosis: feature.Diagnose(grants, scope)}, nil
}

func (uc *diagnoseAccessUseCase) memberScope(in workspace.DiagnoseAccessInput, grants workspace.Grants) (workspace.MemberScope, error) {
	if grants.Manager() {
		return workspace.MemberScope{}, nil
	}
	filter, err := wd.MemberFilter(uc.departments, in.WorkspaceID, in.MemberUserID)
	if err != nil {
		return workspace.MemberScope{}, err
	}
	scope := workspace.MemberScope{WorkspaceUsesDepartments: filter.WorkspaceHasDepartments, DepartmentCount: len(filter.DepartmentIDs)}
	if scope.DepartmentCount == 0 || !uc.mayReadDepartments(in) {
		return scope, nil
	}
	departments, err := uc.departments.ListDepartmentsByIDs(filter.DepartmentIDs)
	if err != nil {
		return workspace.MemberScope{}, err
	}
	for _, d := range departments {
		scope.DepartmentNames = append(scope.DepartmentNames, d.Name)
	}
	return scope, nil
}

func (uc *diagnoseAccessUseCase) mayReadDepartments(in workspace.DiagnoseAccessInput) bool {
	if in.CallerRole == "admin" || in.ActorID == in.MemberUserID {
		return true
	}
	actor := mustBeMember(uc.repo, in.WorkspaceID, in.ActorID)
	if actor == nil {
		return false
	}
	if actor.Role.CanManageMembers() {
		return true
	}
	has, err := uc.repo.HasPermission(actor.ID, workspace.ResourceDepartments, workspace.ActionRead)
	return err == nil && has
}
