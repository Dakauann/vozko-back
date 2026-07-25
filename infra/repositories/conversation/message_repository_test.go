package conversation_repository

import (
	"strings"
	"testing"
)

func TestDepartmentScopeClause_Unrestricted(t *testing.T) {
	clause, args := departmentScopeClause("c.department_id", "ce.id", nil, false, "")
	if clause != "" {
		t.Fatalf("expected empty clause, got %q", clause)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
}

func TestDepartmentScopeClause_NoDepartmentsReturnsNoRows(t *testing.T) {
	clause, args := departmentScopeClause("c.department_id", "ce.id", nil, true, "")
	if clause != " AND 1 = 0" {
		t.Fatalf("expected fail-closed clause, got %q", clause)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args, got %v", args)
	}
}

func TestDepartmentScopeClause_RestrictsToExplicitDepartments(t *testing.T) {
	clause, args := departmentScopeClause("c.department_id", "ce.id", []string{"dept-1"}, true, "")
	if clause != " AND c.department_id = ANY(?::uuid[])" {
		t.Fatalf("expected scoped department clause, got %q", clause)
	}
	if len(args) != 1 {
		t.Fatalf("expected one arg, got %d", len(args))
	}
}

func TestDepartmentScopeClause_AssigneeEscape_WithDepartments(t *testing.T) {
	clause, args := departmentScopeClause("wc.department_id", "wce.id", []string{"dept-1"}, true, "user-B")
	if !strings.Contains(clause, "wc.department_id = ANY(?::uuid[])") {
		t.Fatalf("expected dept ANY check in clause, got %q", clause)
	}
	if !strings.Contains(clause, "EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?)") {
		t.Fatalf("expected EXISTS escape in clause, got %q", clause)
	}
	if !strings.Contains(clause, " OR ") {
		t.Fatalf("expected OR linkage between dept and assignee escape, got %q", clause)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args (dept slice + user id), got %d: %v", len(args), args)
	}
	if args[len(args)-1] != "user-B" {
		t.Fatalf("expected last arg to be assignee user id, got %v", args[len(args)-1])
	}
}

func TestDepartmentScopeClause_AssigneeEscape_EmptyDepartments(t *testing.T) {
	clause, args := departmentScopeClause("wc.department_id", "wce.id", nil, true, "user-B")
	if strings.Contains(clause, "1 = 0") {
		t.Fatalf("must not fall through to fail-closed when assignee escape is present, got %q", clause)
	}
	if !strings.Contains(clause, "EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?)") {
		t.Fatalf("expected EXISTS-only escape, got %q", clause)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg (user id), got %d: %v", len(args), args)
	}
	if args[0] != "user-B" {
		t.Fatalf("expected arg to be assignee user id, got %v", args[0])
	}
}
