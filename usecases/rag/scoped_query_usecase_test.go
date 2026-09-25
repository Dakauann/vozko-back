package rag_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/rag"
	wd "vozko/domain/workspace/workspace_department"
)

type kbGetter map[string]*rag.KnowledgeBase

func (g kbGetter) Execute(_ context.Context, id string) (*rag.KnowledgeBase, error) {
	kb, ok := g[id]
	if !ok {
		return nil, rag.ErrKnowledgeBaseNotFound
	}
	return kb, nil
}

type queryRecorder struct{ inputs []rag.QueryInput }

func (q *queryRecorder) Execute(_ context.Context, in rag.QueryInput) (*rag.QueryOutput, error) {
	q.inputs = append(q.inputs, in)
	return &rag.QueryOutput{TotalFound: 1}, nil
}

var bases = kbGetter{
	"kb-sales":   {ID: "kb-sales", WorkspaceID: "ws1", DepartmentID: "d-sales"},
	"kb-support": {ID: "kb-support", WorkspaceID: "ws1", DepartmentID: "d-support"},
	"kb-other":   {ID: "kb-other", WorkspaceID: "ws2"},
}

func salesMember() rag.Viewer {
	return rag.Viewer{WorkspaceID: "ws1", Departments: &wd.DepartmentFilter{DepartmentIDs: []string{"d-sales"}, WorkspaceHasDepartments: true}}
}

func TestScopedQueryRefusesAnotherWorkspacesBase(t *testing.T) {
	query := &queryRecorder{}
	owner := rag.Viewer{WorkspaceID: "ws1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}
	_, err := NewScopedQueryUseCase(NewKnowledgeBaseAccessUseCase(bases), query).Execute(context.Background(), owner, rag.QueryInput{Query: "preço", KnowledgeBaseIDs: []string{"kb-other"}})
	if !errors.Is(err, rag.ErrKnowledgeBaseAccessDenied) || len(query.inputs) != 0 {
		t.Fatalf("err = %v after %d queries", err, len(query.inputs))
	}
}

func TestScopedQueryRefusesAnotherDepartmentsBase(t *testing.T) {
	query := &queryRecorder{}
	_, err := NewScopedQueryUseCase(NewKnowledgeBaseAccessUseCase(bases), query).Execute(context.Background(), salesMember(), rag.QueryInput{Query: "preço", KnowledgeBaseIDs: []string{"kb-sales", "kb-support"}})
	if !errors.Is(err, rag.ErrKnowledgeBaseAccessDenied) || len(query.inputs) != 0 {
		t.Fatalf("err = %v after %d queries", err, len(query.inputs))
	}
}

func TestScopedQueryFailsClosedWithoutADepartmentFilter(t *testing.T) {
	_, err := NewScopedQueryUseCase(NewKnowledgeBaseAccessUseCase(bases), &queryRecorder{}).Execute(context.Background(), rag.Viewer{WorkspaceID: "ws1"}, rag.QueryInput{Query: "preço", KnowledgeBaseIDs: []string{"kb-sales"}})
	if !errors.Is(err, rag.ErrKnowledgeBaseAccessDenied) {
		t.Fatalf("err = %v", err)
	}
}

func TestScopedQueryReportsAnUnknownBase(t *testing.T) {
	_, err := NewScopedQueryUseCase(NewKnowledgeBaseAccessUseCase(bases), &queryRecorder{}).Execute(context.Background(), salesMember(), rag.QueryInput{Query: "preço", KnowledgeBaseIDs: []string{"nope"}})
	if !errors.Is(err, rag.ErrKnowledgeBaseNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestScopedQueryValidatesAndBoundsTheInput(t *testing.T) {
	uc := NewScopedQueryUseCase(NewKnowledgeBaseAccessUseCase(bases), &queryRecorder{})
	if _, err := uc.Execute(context.Background(), salesMember(), rag.QueryInput{Query: " ", KnowledgeBaseIDs: []string{"kb-sales"}}); !errors.Is(err, rag.ErrQueryRequired) {
		t.Fatalf("blank query: %v", err)
	}
	if _, err := uc.Execute(context.Background(), salesMember(), rag.QueryInput{Query: "x"}); !errors.Is(err, rag.ErrKnowledgeBaseIDRequired) {
		t.Fatalf("no bases: %v", err)
	}
	many := make([]string, rag.MaxQueriedKnowledgeBases+1)
	for i := range many {
		many[i] = "kb-sales"
	}
	if _, err := uc.Execute(context.Background(), salesMember(), rag.QueryInput{Query: "x", KnowledgeBaseIDs: many}); err != nil {
		t.Fatalf("duplicates must collapse: %v", err)
	}
}

func TestScopedQueryPassesDefaultsForTheAllowedBases(t *testing.T) {
	query := &queryRecorder{}
	if _, err := NewScopedQueryUseCase(NewKnowledgeBaseAccessUseCase(bases), query).Execute(context.Background(), salesMember(), rag.QueryInput{Query: " preço ", KnowledgeBaseIDs: []string{"kb-sales", "kb-sales"}, TopK: 99}); err != nil {
		t.Fatal(err)
	}
	in := query.inputs[0]
	if in.Query != "preço" || len(in.KnowledgeBaseIDs) != 1 || in.TopK != rag.MaxQueryResults || in.MinScore != rag.DefaultQueryMinScore {
		t.Fatalf("input = %+v", in)
	}
}
