package rag_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/rag"
)

type documentRecorder struct{ inputs []rag.CreateDocumentInput }

func (d *documentRecorder) Execute(_ context.Context, in rag.CreateDocumentInput) (*rag.Document, error) {
	d.inputs = append(d.inputs, in)
	return &rag.Document{ID: "doc-1", KnowledgeBaseID: in.KnowledgeBaseID}, nil
}

func TestKnowledgeBaseAccessChecksWorkspaceAndDepartment(t *testing.T) {
	access := NewKnowledgeBaseAccessUseCase(bases)
	if kb, err := access.Owned(context.Background(), salesMember(), " kb-sales "); err != nil || kb.ID != "kb-sales" {
		t.Fatalf("own base: %v", err)
	}
	for id, want := range map[string]error{
		"kb-support": rag.ErrKnowledgeBaseAccessDenied,
		"kb-other":   rag.ErrKnowledgeBaseAccessDenied,
		"nope":       rag.ErrKnowledgeBaseNotFound,
		"":           rag.ErrKnowledgeBaseNotFound,
	} {
		if _, err := access.Owned(context.Background(), salesMember(), id); !errors.Is(err, want) {
			t.Fatalf("%q: %v", id, err)
		}
	}
}

func TestScopedDocumentsOnlyAddToAVisibleBase(t *testing.T) {
	docs := &documentRecorder{}
	uc := NewScopedDocumentsUseCase(NewKnowledgeBaseAccessUseCase(bases), docs)
	if _, err := uc.Add(context.Background(), salesMember(), rag.CreateDocumentInput{KnowledgeBaseID: "kb-support", MediaID: "m1"}); !errors.Is(err, rag.ErrKnowledgeBaseAccessDenied) {
		t.Fatalf("other department: %v", err)
	}
	if _, err := uc.Add(context.Background(), rag.Viewer{WorkspaceID: "ws1"}, rag.CreateDocumentInput{KnowledgeBaseID: "kb-sales"}); !errors.Is(err, rag.ErrKnowledgeBaseAccessDenied) {
		t.Fatalf("no department filter: %v", err)
	}
	if len(docs.inputs) != 0 {
		t.Fatalf("documents created: %+v", docs.inputs)
	}
	if _, err := uc.Add(context.Background(), salesMember(), rag.CreateDocumentInput{KnowledgeBaseID: " kb-sales ", MediaID: "m1"}); err != nil || docs.inputs[0].KnowledgeBaseID != "kb-sales" {
		t.Fatalf("own base: %v %+v", err, docs.inputs)
	}
}
