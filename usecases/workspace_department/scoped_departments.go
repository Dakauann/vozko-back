package workspace_department_usecase

import (
	"strings"

	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type MemberLookup interface {
	GetMemberByID(memberID string) (*workspace.Member, error)
}

type scopedDepartments struct {
	get     wd.GetDepartmentUseCase
	update  wd.UpdateDepartmentUseCase
	remove  wd.DeleteDepartmentUseCase
	members wd.ListMembersUseCase
	add     wd.AddMemberUseCase
	unlink  wd.RemoveMemberUseCase
	lookup  MemberLookup
}

func NewScopedDepartmentsUseCase(repo wd.Repository, lookup MemberLookup) wd.ScopedDepartmentsUseCase {
	return &scopedDepartments{
		get:     NewGetDepartmentUseCase(repo),
		update:  NewUpdateDepartmentUseCase(repo),
		remove:  NewDeleteDepartmentUseCase(repo),
		members: NewListMembersUseCase(repo),
		add:     NewAddMemberUseCase(repo),
		unlink:  NewRemoveMemberUseCase(repo),
		lookup:  lookup,
	}
}

func (uc *scopedDepartments) owned(workspaceID, departmentID string) (*wd.Department, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, wd.ErrDepartmentNotFound
	}
	d, err := uc.get.Execute(departmentID)
	if err != nil || d == nil || d.WorkspaceID != workspaceID {
		return nil, wd.ErrDepartmentNotFound
	}
	return d, nil
}

func (uc *scopedDepartments) Get(workspaceID, departmentID string) (*wd.Department, error) {
	return uc.owned(workspaceID, departmentID)
}

func (uc *scopedDepartments) Update(workspaceID, departmentID string, input wd.UpdateDepartmentInput) (*wd.Department, error) {
	if _, err := uc.owned(workspaceID, departmentID); err != nil {
		return nil, err
	}
	return uc.update.Execute(departmentID, input)
}

func (uc *scopedDepartments) Delete(workspaceID, departmentID string) error {
	if _, err := uc.owned(workspaceID, departmentID); err != nil {
		return err
	}
	return uc.remove.Execute(departmentID)
}

func (uc *scopedDepartments) ListMembers(workspaceID, departmentID string) ([]wd.DepartmentMember, error) {
	if _, err := uc.owned(workspaceID, departmentID); err != nil {
		return nil, err
	}
	return uc.members.Execute(departmentID)
}

func (uc *scopedDepartments) AddMember(workspaceID, departmentID string, input wd.AddMemberInput) (*wd.DepartmentMember, error) {
	if _, err := uc.owned(workspaceID, departmentID); err != nil {
		return nil, err
	}
	member, err := uc.lookup.GetMemberByID(strings.TrimSpace(input.MemberID))
	if err != nil || member == nil || member.WorkspaceID != workspaceID {
		return nil, workspace.ErrMemberNotFound
	}
	return uc.add.Execute(departmentID, wd.AddMemberInput{MemberID: member.ID})
}

func (uc *scopedDepartments) RemoveMember(workspaceID, departmentID, memberID string) error {
	if _, err := uc.owned(workspaceID, departmentID); err != nil {
		return err
	}
	return uc.unlink.Execute(departmentID, memberID)
}
