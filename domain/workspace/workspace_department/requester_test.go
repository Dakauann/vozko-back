package workspace_department

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/workspace"
)

func TestNarrow(t *testing.T) {
	cases := []struct {
		name      string
		filter    *DepartmentFilter
		requested []string
		want      []string
		none      bool
	}{
		{"an owner keeps what was asked", &DepartmentFilter{IsOwnerOrAdmin: true}, []string{"d9"}, []string{"d9"}, false},
		{"an owner who asked for nothing sees everything", &DepartmentFilter{IsOwnerOrAdmin: true}, nil, nil, false},
		{"a workspace without departments keeps what was asked", &DepartmentFilter{}, []string{"d1"}, []string{"d1"}, false},
		{"a member who asked for nothing gets its own departments", &DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}, WorkspaceHasDepartments: true}, nil, []string{"d1", "d2"}, false},
		{"a member never widens its scope", &DepartmentFilter{DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true}, []string{"d1", "d9"}, []string{"d1"}, false},
		{"a member asking only for another department gets nothing", &DepartmentFilter{DepartmentIDs: []string{"d1"}, WorkspaceHasDepartments: true}, []string{"d9"}, nil, true},
		{"a member without departments gets nothing", &DepartmentFilter{WorkspaceHasDepartments: true}, nil, nil, true},
		{"no filter at all is nothing", nil, []string{"d1"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, none := tc.filter.Narrow(tc.requested)
			if none != tc.none || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Narrow = %v, %v; want %v, %v", got, none, tc.want, tc.none)
			}
		})
	}
}

type departmentsOf map[string][]string

func (d departmentsOf) GetMemberDepartmentIDs(_, userID string) ([]string, error) {
	if userID == "broken" {
		return nil, errors.New("down")
	}
	return d[userID], nil
}

func (d departmentsOf) ListDepartments(string) ([]Department, error) {
	return []Department{{ID: "d1"}}, nil
}

func TestRequesterFilter(t *testing.T) {
	source := departmentsOf{"member": {"d1"}}
	owner, err := RequesterFilter(true, source, "ws", "owner")
	if err != nil || !owner.IsOwnerOrAdmin {
		t.Fatalf("owner = %+v, %v", owner, err)
	}
	member, err := RequesterFilter(false, source, "ws", "member")
	if err != nil || !reflect.DeepEqual(member.DepartmentIDs, []string{"d1"}) || member.IsOwnerOrAdmin {
		t.Fatalf("member = %+v, %v", member, err)
	}
	if _, err := RequesterFilter(false, source, "ws", "broken"); err == nil {
		t.Fatal("an unreadable scope must refuse")
	}
}

type membersOf map[string]*workspace.Member

func (m membersOf) GetMember(_, userID string) (*workspace.Member, error) {
	if userID == "broken" {
		return nil, errors.New("down")
	}
	return m[userID], nil
}

func TestSeesEveryDepartment(t *testing.T) {
	members := membersOf{
		"owner":  {Role: workspace.RoleOwner},
		"admin":  {Role: workspace.RoleAdmin},
		"member": {Role: workspace.RoleMember},
	}
	cases := []struct {
		name          string
		members       Members
		userID        string
		platformAdmin bool
		want          bool
		fails         bool
	}{
		{"a platform admin", members, "anyone", true, true, false},
		{"a workspace owner", members, "owner", false, true, false},
		{"a workspace admin", members, "admin", false, true, false},
		{"a member", members, "member", false, false, false},
		{"someone who is not a member", members, "stranger", false, false, false},
		{"an unreadable membership", members, "broken", false, false, true},
		{"no member directory", nil, "owner", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SeesEveryDepartment(tc.members, "ws", tc.userID, tc.platformAdmin)
			if got != tc.want || (err != nil) != tc.fails {
				t.Fatalf("SeesEveryDepartment = %v, %v", got, err)
			}
		})
	}
}

func TestRequesterScope(t *testing.T) {
	source := departmentsOf{"member": {"d1"}}
	members := membersOf{"owner": {Role: workspace.RoleOwner}, "member": {Role: workspace.RoleMember}}
	owner, err := RequesterScope(members, source, "ws", "owner", false)
	if err != nil || !owner.IsOwnerOrAdmin {
		t.Fatalf("owner = %+v, %v", owner, err)
	}
	member, err := RequesterScope(members, source, "ws", "member", false)
	if err != nil || member.IsOwnerOrAdmin || !reflect.DeepEqual(member.DepartmentIDs, []string{"d1"}) {
		t.Fatalf("member = %+v, %v", member, err)
	}
	if _, err := RequesterScope(members, source, "ws", "broken", false); err == nil {
		t.Fatal("an unreadable membership must refuse")
	}
}
