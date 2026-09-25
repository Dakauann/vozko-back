package rag_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/rag"
	wd "vozko/domain/workspace/workspace_department"
)

type kbRepoStub struct {
	rag.KnowledgeBaseRepository
	created []*rag.KnowledgeBase
}

func (r *kbRepoStub) CountByWorkspace(context.Context, string) (int, error) { return 0, nil }

func (r *kbRepoStub) Create(_ context.Context, kb *rag.KnowledgeBase) error {
	r.created = append(r.created, kb)
	return nil
}

type departmentResolverStub struct {
	id  string
	err error
}

func (d departmentResolverStub) Resolve(context.Context, string) (string, error) { return d.id, d.err }

func TestCreateKnowledgeBaseLandsInTheCreatorsDepartment(t *testing.T) {
	repo := &kbRepoStub{}
	kb, err := NewCreateKnowledgeBaseUseCase(repo, departmentResolverStub{id: "d-sales"}).Execute(context.Background(), rag.CreateKnowledgeBaseInput{WorkspaceID: "ws1", Name: "Preços"})
	if err != nil || kb.DepartmentID != "d-sales" {
		t.Fatalf("kb %+v err %v", kb, err)
	}
}

func TestCreateKnowledgeBaseRefusesWhenTheDepartmentCannotBeResolved(t *testing.T) {
	repo := &kbRepoStub{}
	if _, err := NewCreateKnowledgeBaseUseCase(repo, departmentResolverStub{err: wd.ErrDepartmentRequired}).Execute(context.Background(), rag.CreateKnowledgeBaseInput{WorkspaceID: "ws1", Name: "x"}); !errors.Is(err, wd.ErrDepartmentRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewCreateKnowledgeBaseUseCase(repo, nil).Execute(context.Background(), rag.CreateKnowledgeBaseInput{WorkspaceID: "ws1", Name: "x"}); !errors.Is(err, wd.ErrDepartmentAccessDenied) {
		t.Fatalf("no resolver: err = %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("created without a department decision")
	}
}
