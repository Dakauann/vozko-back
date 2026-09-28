package workspace_department_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type membersByID map[string]*workspace.Member

func (m membersByID) GetMemberByID(id string) (*workspace.Member, error) {
	if member, ok := m[id]; ok {
		return member, nil
	}
	return nil, workspace.ErrMemberNotFound
}

func scopedFixture() (wd.ScopedDepartmentsUseCase, *mockRepository) {
	repo := newMockRepo()
	repo.departments["d-sales"] = &wd.Department{ID: "d-sales", WorkspaceID: "ws1", Name: "Vendas"}
	repo.departments["d-foreign"] = &wd.Department{ID: "d-foreign", WorkspaceID: "ws2", Name: "Outro"}
	members := membersByID{
		"m-ana":     {ID: "m-ana", WorkspaceID: "ws1", UserID: "u-ana"},
		"m-foreign": {ID: "m-foreign", WorkspaceID: "ws2", UserID: "u-x"},
	}
	return NewScopedDepartmentsUseCase(repo, members), repo
}

func TestScopedDepartmentsHideAnotherWorkspacesDepartment(t *testing.T) {
	uc, repo := scopedFixture()
	name := "Invadido"
	checks := map[string]error{}
	_, checks["get"] = uc.Get("ws1", "d-foreign")
	_, checks["update"] = uc.Update("ws1", "d-foreign", wd.UpdateDepartmentInput{Name: &name})
	checks["delete"] = uc.Delete("ws1", "d-foreign")
	_, checks["members"] = uc.ListMembers("ws1", "d-foreign")
	_, checks["add"] = uc.AddMember("ws1", "d-foreign", wd.AddMemberInput{MemberID: "m-ana"})
	checks["remove"] = uc.RemoveMember("ws1", "d-foreign", "m-ana")
	for action, err := range checks {
		if !errors.Is(err, wd.ErrDepartmentNotFound) {
			t.Errorf("%s: %v", action, err)
		}
	}
	if repo.departments["d-foreign"].Name != "Outro" {
		t.Fatal("another workspace's department was changed")
	}
}

func TestScopedDepartmentsOnlyAddMembersOfTheSameWorkspace(t *testing.T) {
	uc, _ := scopedFixture()
	if _, err := uc.AddMember("ws1", "d-sales", wd.AddMemberInput{MemberID: "m-foreign"}); !errors.Is(err, workspace.ErrMemberNotFound) {
		t.Fatalf("foreign member: %v", err)
	}
	if _, err := uc.AddMember("ws1", "d-sales", wd.AddMemberInput{MemberID: "nobody"}); !errors.Is(err, workspace.ErrMemberNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
	if dm, err := uc.AddMember("ws1", "d-sales", wd.AddMemberInput{MemberID: "m-ana"}); err != nil || dm.MemberID != "m-ana" {
		t.Fatalf("own member: %+v %v", dm, err)
	}
}

func TestScopedDepartmentsWorkOnTheWorkspacesOwnDepartment(t *testing.T) {
	uc, _ := scopedFixture()
	name := "Vendas B2B"
	if d, err := uc.Update("ws1", "d-sales", wd.UpdateDepartmentInput{Name: &name}); err != nil || d.Name != name {
		t.Fatalf("update: %+v %v", d, err)
	}
	if _, err := uc.AddMember("ws1", "d-sales", wd.AddMemberInput{MemberID: "m-ana"}); err != nil {
		t.Fatal(err)
	}
	if err := uc.RemoveMember("ws1", "d-sales", "m-ana"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := uc.Delete("ws1", "d-sales"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
