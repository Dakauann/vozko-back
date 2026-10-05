package workspace_department

import (
	"errors"
	"reflect"
	"testing"
)

var (
	owner           = &DepartmentFilter{IsOwnerOrAdmin: true}
	flatMember      = &DepartmentFilter{}
	oneDeptMember   = &DepartmentFilter{DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true}
	twoDeptMember   = &DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}, WorkspaceHasDepartments: true}
	strandedMember  = &DepartmentFilter{WorkspaceHasDepartments: true}
	selectingMember = &DepartmentFilter{DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true, SelectedDepartmentID: strPtr("d9")}
)

func TestAllows(t *testing.T) {
	cases := []struct {
		name   string
		filter *DepartmentFilter
		dept   string
		want   bool
	}{
		{"no filter denies", nil, "d1", false},
		{"owner sees any department", owner, "d7", true},
		{"owner sees unassigned resources", owner, "", true},
		{"member of a workspace without departments sees everything", flatMember, "d7", true},
		{"member sees own department", twoDeptMember, "d2", true},
		{"member does not see another department", oneDeptMember, "d2", false},
		{"restricted member does not see unassigned resources", oneDeptMember, "", false},
		{"member in no department sees nothing", strandedMember, "d1", false},
		{"selection header does not grant a department", selectingMember, "d9", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.Allows(tc.dept); got != tc.want {
				t.Fatalf("Allows(%q) = %v, want %v", tc.dept, got, tc.want)
			}
		})
	}
}

func TestListScope(t *testing.T) {
	cases := []struct {
		name        string
		filter      *DepartmentFilter
		wantIDs     []string
		wantBlocked bool
	}{
		{"no filter is blocked", nil, nil, true},
		{"owner is unscoped", owner, nil, false},
		{"flat workspace member is unscoped", flatMember, nil, false},
		{"member is scoped to own departments", twoDeptMember, []string{"d1", "d2"}, false},
		{"member in no department is blocked", strandedMember, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids, blocked := tc.filter.ListScope()
			if blocked != tc.wantBlocked || !reflect.DeepEqual(ids, tc.wantIDs) {
				t.Fatalf("ListScope() = %v, %v; want %v, %v", ids, blocked, tc.wantIDs, tc.wantBlocked)
			}
		})
	}
}

func TestReadScope(t *testing.T) {
	cases := []struct {
		name      string
		filter    *DepartmentFilter
		requested string
		want      string
		wantErr   error
	}{
		{"no filter is denied", nil, "", "", ErrDepartmentAccessDenied},
		{"owner reads the whole workspace", owner, "", "", nil},
		{"owner narrows to a department", owner, "d3", "d3", nil},
		{"flat workspace member reads the whole workspace", flatMember, "", "", nil},
		{"single-department member defaults to it", oneDeptMember, "", "d1", nil},
		{"member names own department", twoDeptMember, "d2", "d2", nil},
		{"multi-department member must choose", twoDeptMember, "", "", ErrDepartmentRequired},
		{"member cannot read another department", twoDeptMember, "d3", "", ErrDepartmentAccessDenied},
		{"member in no department is denied", strandedMember, "", "", ErrDepartmentAccessDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.filter.ReadScope(tc.requested)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("ReadScope(%q) = %q, %v; want %q, %v", tc.requested, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestSeesWholeWorkspace(t *testing.T) {
	cases := []struct {
		name   string
		filter *DepartmentFilter
		want   bool
	}{
		{"no filter", nil, false},
		{"owner", owner, true},
		{"flat workspace member", flatMember, true},
		{"department member", oneDeptMember, false},
		{"member in no department", strandedMember, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.SeesWholeWorkspace(); got != tc.want {
				t.Fatalf("SeesWholeWorkspace() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAViewerSeesAMemberOnlyThroughASharedDepartment(t *testing.T) {
	restricted := &DepartmentFilter{DepartmentIDs: []string{"vendas"}, WorkspaceHasDepartments: true}
	cases := []struct {
		name   string
		filter *DepartmentFilter
		member []string
		want   bool
	}{
		{"shares a department", restricted, []string{"suporte", "vendas"}, true},
		{"different department", restricted, []string{"suporte"}, false},
		{"member without department", restricted, nil, false},
		{"owner or admin", &DepartmentFilter{IsOwnerOrAdmin: true, WorkspaceHasDepartments: true}, []string{"suporte"}, true},
		{"workspace without departments", &DepartmentFilter{}, nil, true},
		{"viewer blocked without department", &DepartmentFilter{WorkspaceHasDepartments: true}, []string{"vendas"}, false},
		{"no filter", nil, []string{"vendas"}, false},
	}
	for _, tc := range cases {
		if got := tc.filter.AllowsMember(tc.member); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
