package rag_usecase

import (
	"context"
	"strings"

	"vozko/domain/rag"
)

type knowledgeBaseAccess struct{ get rag.GetKnowledgeBaseUseCase }

func NewKnowledgeBaseAccessUseCase(get rag.GetKnowledgeBaseUseCase) rag.KnowledgeBaseAccessUseCase {
	return &knowledgeBaseAccess{get: get}
}

func (uc *knowledgeBaseAccess) Owned(ctx context.Context, viewer rag.Viewer, id string) (*rag.KnowledgeBase, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, rag.ErrKnowledgeBaseNotFound
	}
	kb, err := uc.get.Execute(ctx, id)
	if err != nil {
		return nil, err
	}
	if !viewer.CanRead(kb) {
		return nil, rag.ErrKnowledgeBaseAccessDenied
	}
	return kb, nil
}

type scopedDocuments struct {
	access rag.KnowledgeBaseAccessUseCase
	create rag.CreateDocumentUseCase
}

func NewScopedDocumentsUseCase(access rag.KnowledgeBaseAccessUseCase, create rag.CreateDocumentUseCase) rag.ScopedDocumentsUseCase {
	return &scopedDocuments{access: access, create: create}
}

func (uc *scopedDocuments) Add(ctx context.Context, viewer rag.Viewer, input rag.CreateDocumentInput) (*rag.Document, error) {
	kb, err := uc.access.Owned(ctx, viewer, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	input.KnowledgeBaseID = kb.ID
	return uc.create.Execute(ctx, input)
}
