package workspace_department

import (
	"errors"
	"testing"
)

type departmentsStub struct {
	memberIDs  []string
	memberErr  error
	all        []Department
	listErr    error
	listCalled bool
}

func (s *departmentsStub) GetMemberDepartmentIDs(string, string) ([]string, error) {
	return s.memberIDs, s.memberErr
}

func (s *departmentsStub) ListDepartments(string) ([]Department, error) {
	s.listCalled = true
	return s.all, s.listErr
}

func TestMemberFilterScopesAMemberToTheirDepartments(t *testing.T) {
	stub := &departmentsStub{memberIDs: []string{"d1"}}
	f, err := MemberFilter(stub, "ws", "u")
	if err != nil || !f.ShouldFilter() || len(f.DepartmentIDs) != 1 || stub.listCalled {
		t.Fatalf("got %+v, %v", f, err)
	}
}

func TestMemberFilterBlocksAMemberOutsideEveryDepartment(t *testing.T) {
	f, err := MemberFilter(&departmentsStub{all: []Department{{ID: "d1"}}}, "ws", "u")
	if err != nil || !f.BlockedByMissingDepartment() {
		t.Fatalf("got %+v, %v", f, err)
	}
}

func TestMemberFilterLeavesWorkspacesWithoutDepartmentsUnscoped(t *testing.T) {
	f, err := MemberFilter(&departmentsStub{}, "ws", "u")
	if err != nil || f.ShouldFilter() {
		t.Fatalf("got %+v, %v", f, err)
	}
}

func TestMemberFilterReportsLookupFailures(t *testing.T) {
	boom := errors.New("boom")
	if _, err := MemberFilter(&departmentsStub{memberErr: boom}, "ws", "u"); !errors.Is(err, boom) {
		t.Fatalf("member lookup failure must surface, got %v", err)
	}
	if _, err := MemberFilter(&departmentsStub{listErr: boom}, "ws", "u"); !errors.Is(err, boom) {
		t.Fatalf("department listing failure must surface, got %v", err)
	}
}

func TestBlockedFilterShowsNothing(t *testing.T) {
	if !BlockedFilter().BlockedByMissingDepartment() {
		t.Fatal("the fallback filter must hide every department-scoped item")
	}
}
