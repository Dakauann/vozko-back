package rag

import "context"

type KnowledgeBaseAccessUseCase interface {
	Owned(ctx context.Context, viewer Viewer, id string) (*KnowledgeBase, error)
}

type ScopedDocumentsUseCase interface {
	Add(ctx context.Context, viewer Viewer, input CreateDocumentInput) (*Document, error)
}
