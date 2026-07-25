package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type workflowDepartmentResolverStub struct {
	departmentID string
	err          error
	workspaceID  string
}

func (s *workflowDepartmentResolverStub) Resolve(_ context.Context, workspaceID string) (string, error) {
	s.workspaceID = workspaceID
	if s.err != nil {
		return "", s.err
	}
	return s.departmentID, nil
}

func TestAssignDepartmentUseCase_AssignsResolvedDepartment(t *testing.T) {
	repo := NewMockWorkflowRepository()
	repo.workflows["workflow-1"] = &workflow.Workflow{ID: "workflow-1", WorkspaceID: "ws-1"}
	resolver := &workflowDepartmentResolverStub{departmentID: "dept-1"}

	uc := NewAssignDepartmentUseCase(repo, resolver)
	updated, err := uc.Execute(context.Background(), "workflow-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolver.workspaceID != "ws-1" {
		t.Fatalf("expected resolver workspace ws-1, got %s", resolver.workspaceID)
	}
	if updated.DepartmentID != "dept-1" {
		t.Fatalf("expected assigned department dept-1, got %s", updated.DepartmentID)
	}
	if repo.workflows["workflow-1"].DepartmentID != "dept-1" {
		t.Fatalf("expected repository to persist dept-1, got %s", repo.workflows["workflow-1"].DepartmentID)
	}
}

func TestAssignDepartmentUseCase_ReturnsResolverError(t *testing.T) {
	repo := NewMockWorkflowRepository()
	repo.workflows["workflow-1"] = &workflow.Workflow{ID: "workflow-1", WorkspaceID: "ws-1"}
	resolver := &workflowDepartmentResolverStub{err: errors.New("resolver failed")}

	uc := NewAssignDepartmentUseCase(repo, resolver)
	_, err := uc.Execute(context.Background(), "workflow-1")
	if err == nil || err.Error() != "resolver failed" {
		t.Fatalf("expected resolver error, got %v", err)
	}
	if repo.workflows["workflow-1"].DepartmentID != "" {
		t.Fatalf("expected repository department to remain empty, got %s", repo.workflows["workflow-1"].DepartmentID)
	}
}
