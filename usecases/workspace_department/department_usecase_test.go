package workspace_department_usecase

import (
	"testing"

	wd "vozko/domain/workspace/workspace_department"
)

func TestCreateDepartment_Success(t *testing.T) {
	repo := newMockRepo()
	uc := NewCreateDepartmentUseCase(repo)

	dept, err := uc.Execute(wd.CreateDepartmentInput{
		WorkspaceID: "ws-1",
		Name:        "Sales",
		Description: "Sales team",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dept.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if dept.Name != "Sales" {
		t.Errorf("Name = %q, want %q", dept.Name, "Sales")
	}
	if dept.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID = %q, want %q", dept.WorkspaceID, "ws-1")
	}

	stored, err := repo.GetDepartmentByID(dept.ID)
	if err != nil {
		t.Fatalf("department not found in repo: %v", err)
	}
	if stored.Name != "Sales" {
		t.Errorf("stored Name = %q, want %q", stored.Name, "Sales")
	}
}

func TestCreateDepartment_NameRequired(t *testing.T) {
	repo := newMockRepo()
	uc := NewCreateDepartmentUseCase(repo)

	_, err := uc.Execute(wd.CreateDepartmentInput{
		WorkspaceID: "ws-1",
		Name:        "",
	})
	if err != wd.ErrDepartmentNameRequired {
		t.Fatalf("error = %v, want ErrDepartmentNameRequired", err)
	}
}

func TestCreateDepartment_WhitespaceOnlyName(t *testing.T) {
	repo := newMockRepo()
	uc := NewCreateDepartmentUseCase(repo)

	_, err := uc.Execute(wd.CreateDepartmentInput{
		WorkspaceID: "ws-1",
		Name:        "   ",
	})
	if err != wd.ErrDepartmentNameRequired {
		t.Fatalf("error = %v, want ErrDepartmentNameRequired", err)
	}
}

func TestGetDepartment_Success(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	getUC := NewGetDepartmentUseCase(repo)

	created, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Support"})

	dept, err := getUC.Execute(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dept.Name != "Support" {
		t.Errorf("Name = %q, want %q", dept.Name, "Support")
	}
}

func TestGetDepartment_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetDepartmentUseCase(repo)

	_, err := uc.Execute("non-existent")
	if err != wd.ErrDepartmentNotFound {
		t.Fatalf("error = %v, want ErrDepartmentNotFound", err)
	}
}

func TestListDepartments_FiltersByWorkspace(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	listUC := NewListDepartmentsUseCase(repo)

	createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})
	createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Support"})
	createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-2", Name: "Other"})

	list, err := listUC.Execute("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestListDepartments_EmptyWorkspace(t *testing.T) {
	repo := newMockRepo()
	uc := NewListDepartmentsUseCase(repo)

	list, err := uc.Execute("ws-empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("len = %d, want 0", len(list))
	}
}

func TestUpdateDepartment_Success(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	updateUC := NewUpdateDepartmentUseCase(repo)
	getUC := NewGetDepartmentUseCase(repo)

	created, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Old"})

	newName := "New Name"
	newDesc := "Updated desc"
	updated, err := updateUC.Execute(created.ID, wd.UpdateDepartmentInput{
		Name:        &newName,
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("Name = %q, want %q", updated.Name, "New Name")
	}

	fetched, _ := getUC.Execute(created.ID)
	if fetched.Description != "Updated desc" {
		t.Errorf("Description = %q, want %q", fetched.Description, "Updated desc")
	}
}

func TestUpdateDepartment_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewUpdateDepartmentUseCase(repo)

	name := "x"
	_, err := uc.Execute("non-existent", wd.UpdateDepartmentInput{Name: &name})
	if err != wd.ErrDepartmentNotFound {
		t.Fatalf("error = %v, want ErrDepartmentNotFound", err)
	}
}

func TestUpdateDepartment_EmptyNameRejected(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	updateUC := NewUpdateDepartmentUseCase(repo)

	created, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Valid"})

	empty := ""
	_, err := updateUC.Execute(created.ID, wd.UpdateDepartmentInput{Name: &empty})
	if err != wd.ErrDepartmentNameRequired {
		t.Fatalf("error = %v, want ErrDepartmentNameRequired", err)
	}
}

func TestUpdateDepartment_PartialUpdate(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	updateUC := NewUpdateDepartmentUseCase(repo)
	getUC := NewGetDepartmentUseCase(repo)

	created, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Original", Description: "Desc"})

	newName := "Updated"
	updateUC.Execute(created.ID, wd.UpdateDepartmentInput{Name: &newName})

	fetched, _ := getUC.Execute(created.ID)
	if fetched.Name != "Updated" {
		t.Errorf("Name = %q, want %q", fetched.Name, "Updated")
	}
	if fetched.Description != "Desc" {
		t.Errorf("Description should remain %q, got %q", "Desc", fetched.Description)
	}
}

func TestDeleteDepartment_Success(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	deleteUC := NewDeleteDepartmentUseCase(repo)
	getUC := NewGetDepartmentUseCase(repo)

	created, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "ToDelete"})

	if err := deleteUC.Execute(created.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := getUC.Execute(created.ID)
	if err != wd.ErrDepartmentNotFound {
		t.Fatalf("department should be deleted, error = %v", err)
	}
}

func TestDeleteDepartment_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewDeleteDepartmentUseCase(repo)

	err := uc.Execute("non-existent")
	if err != wd.ErrDepartmentNotFound {
		t.Fatalf("error = %v, want ErrDepartmentNotFound", err)
	}
}

func TestDeleteDepartment_CascadesMembers(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	addMemberUC := NewAddMemberUseCase(repo)
	deleteUC := NewDeleteDepartmentUseCase(repo)
	listMembersUC := NewListMembersUseCase(repo)

	dept, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Dept"})
	addMemberUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "m1"})
	addMemberUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "m2"})

	deleteUC.Execute(dept.ID)

	members, _ := listMembersUC.Execute(dept.ID)
	if len(members) != 0 {
		t.Errorf("members should be empty after delete, got %d", len(members))
	}
}

func TestAddMember_Success(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	addUC := NewAddMemberUseCase(repo)

	dept, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})

	dm, err := addUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "member-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dm.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if dm.DepartmentID != dept.ID {
		t.Errorf("DepartmentID = %q, want %q", dm.DepartmentID, dept.ID)
	}
	if dm.MemberID != "member-1" {
		t.Errorf("MemberID = %q, want %q", dm.MemberID, "member-1")
	}
}

func TestAddMember_DuplicateRejected(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	addUC := NewAddMemberUseCase(repo)

	dept, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})
	addUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "member-1"})

	_, err := addUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "member-1"})
	if err != wd.ErrDepartmentMemberExists {
		t.Fatalf("error = %v, want ErrDepartmentMemberExists", err)
	}
}

func TestAddMember_DepartmentNotFound(t *testing.T) {
	repo := newMockRepo()
	addUC := NewAddMemberUseCase(repo)

	_, err := addUC.Execute("non-existent", wd.AddMemberInput{MemberID: "m1"})
	if err != wd.ErrDepartmentNotFound {
		t.Fatalf("error = %v, want ErrDepartmentNotFound", err)
	}
}

func TestRemoveMember_Success(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	addUC := NewAddMemberUseCase(repo)
	removeUC := NewRemoveMemberUseCase(repo)
	listUC := NewListMembersUseCase(repo)

	dept, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})
	addUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "m1"})

	if err := removeUC.Execute(dept.ID, "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	members, _ := listUC.Execute(dept.ID)
	if len(members) != 0 {
		t.Errorf("members after remove = %d, want 0", len(members))
	}
}

func TestRemoveMember_NotFound(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	removeUC := NewRemoveMemberUseCase(repo)

	dept, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})

	err := removeUC.Execute(dept.ID, "non-existent")
	if err != wd.ErrDepartmentMemberNotFound {
		t.Fatalf("error = %v, want ErrDepartmentMemberNotFound", err)
	}
}

func TestListMembers_Success(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	addUC := NewAddMemberUseCase(repo)
	listUC := NewListMembersUseCase(repo)

	dept, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})
	addUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "m1"})
	addUC.Execute(dept.ID, wd.AddMemberInput{MemberID: "m2"})

	members, err := listUC.Execute(dept.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("len = %d, want 2", len(members))
	}
}

func TestListMembers_Empty(t *testing.T) {
	repo := newMockRepo()
	listUC := NewListMembersUseCase(repo)

	members, err := listUC.Execute("dept-empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("len = %d, want 0", len(members))
	}
}

func TestMemberInMultipleDepartments(t *testing.T) {
	repo := newMockRepo()
	createUC := NewCreateDepartmentUseCase(repo)
	addUC := NewAddMemberUseCase(repo)

	sales, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Sales"})
	support, _ := createUC.Execute(wd.CreateDepartmentInput{WorkspaceID: "ws-1", Name: "Support"})

	addUC.Execute(sales.ID, wd.AddMemberInput{MemberID: "m1"})
	addUC.Execute(support.ID, wd.AddMemberInput{MemberID: "m1"})

	isSales, _ := repo.IsMember(sales.ID, "m1")
	isSupport, _ := repo.IsMember(support.ID, "m1")
	if !isSales || !isSupport {
		t.Fatalf("member should be in both departments: sales=%v, support=%v", isSales, isSupport)
	}

	repo.RemoveMember(sales.ID, "m1")
	isSales, _ = repo.IsMember(sales.ID, "m1")
	isSupport, _ = repo.IsMember(support.ID, "m1")
	if isSales {
		t.Error("member should NOT be in sales after removal")
	}
	if !isSupport {
		t.Error("member should still be in support")
	}
}

func TestDepartmentFilter_InboxScoping(t *testing.T) {

	filter := &wd.DepartmentFilter{
		DepartmentIDs: []string{"dept-sales"},
	}

	if !filter.ShouldFilter() {
		t.Error("member with departments should be filtered")
	}

	ids := filter.EffectiveDepartmentIDs()
	if len(ids) != 1 || ids[0] != "dept-sales" {
		t.Errorf("EffectiveDepartmentIDs = %v, want [dept-sales]", ids)
	}

	selected := "dept-sales"
	filter.SelectedDepartmentID = &selected
	ids = filter.EffectiveDepartmentIDs()
	if len(ids) != 1 || ids[0] != "dept-sales" {
		t.Errorf("with selection EffectiveDepartmentIDs = %v, want [dept-sales]", ids)
	}
}

func TestDepartmentFilter_OwnerBypassesForRoulette(t *testing.T) {

	filter := &wd.DepartmentFilter{
		IsOwnerOrAdmin: true,
		DepartmentIDs:  []string{"dept-1", "dept-2"},
	}

	if filter.ShouldFilter() {
		t.Error("owner should bypass filtering")
	}

	deptID, err := filter.DepartmentIDForCreation()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deptID != "" {
		t.Errorf("owner without selection should create at workspace level, got %q", deptID)
	}
}

func TestDepartmentFilter_MultipleDepartmentsRequiresSelection(t *testing.T) {

	filter := &wd.DepartmentFilter{
		DepartmentIDs: []string{"dept-sales", "dept-support"},
	}

	_, err := filter.DepartmentIDForCreation()
	if err != wd.ErrDepartmentRequired {
		t.Fatalf("error = %v, want ErrDepartmentRequired", err)
	}

	selected := "dept-sales"
	filter.SelectedDepartmentID = &selected
	deptID, err := filter.DepartmentIDForCreation()
	if err != nil {
		t.Fatalf("unexpected error after selection: %v", err)
	}
	if deptID != "dept-sales" {
		t.Errorf("deptID = %q, want dept-sales", deptID)
	}
}

func TestDepartmentFilter_NoDepartmentsInWorkspaceWithoutDepartments(t *testing.T) {

	filter := &wd.DepartmentFilter{
		DepartmentIDs: nil,
	}

	if filter.ShouldFilter() {
		t.Error("workspace without departments should not enter filtering path")
	}

	ids := filter.EffectiveDepartmentIDs()
	if len(ids) != 0 {
		t.Errorf("EffectiveDepartmentIDs should be empty, got %v", ids)
	}

	deptID, err := filter.DepartmentIDForCreation()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deptID != "" {
		t.Errorf("no depts should create at workspace level, got %q", deptID)
	}
}

func TestDepartmentFilter_NoDepartmentsInDepartmentAwareWorkspaceDenied(t *testing.T) {
	filter := &wd.DepartmentFilter{WorkspaceHasDepartments: true}

	if !filter.ShouldFilter() {
		t.Error("department-aware workspace should keep filtering enabled")
	}

	ids := filter.EffectiveDepartmentIDs()
	if len(ids) != 0 {
		t.Errorf("EffectiveDepartmentIDs should be empty, got %v", ids)
	}

	_, err := filter.DepartmentIDForCreation()
	if err != wd.ErrDepartmentAccessDenied {
		t.Fatalf("error = %v, want ErrDepartmentAccessDenied", err)
	}
}

func TestDepartmentFilter_SingleDepartmentAutoSelect(t *testing.T) {

	filter := &wd.DepartmentFilter{
		DepartmentIDs: []string{"dept-only"},
	}

	deptID, err := filter.DepartmentIDForCreation()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deptID != "dept-only" {
		t.Errorf("single dept auto-select = %q, want dept-only", deptID)
	}
}
