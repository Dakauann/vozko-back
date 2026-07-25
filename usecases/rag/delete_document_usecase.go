package rag_usecase

import (
	"context"

	"vozko/domain/rag"
)

type deleteDocumentUseCase struct {
	docRepo    rag.DocumentRepository
	chunkRepo  rag.ChunkRepository
	vectorRepo rag.VectorRepository
	kbRepo     rag.KnowledgeBaseRepository
}

func NewDeleteDocumentUseCase(
	docRepo rag.DocumentRepository,
	chunkRepo rag.ChunkRepository,
	vectorRepo rag.VectorRepository,
	kbRepo rag.KnowledgeBaseRepository,
) rag.DeleteDocumentUseCase {
	return &deleteDocumentUseCase{
		docRepo:    docRepo,
		chunkRepo:  chunkRepo,
		vectorRepo: vectorRepo,
		kbRepo:     kbRepo,
	}
}

func (uc *deleteDocumentUseCase) Execute(ctx context.Context, id string) error {
	doc, err := uc.docRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if err := uc.vectorRepo.DeleteByDocument(ctx, id); err != nil {
		return err
	}

	if err := uc.chunkRepo.DeleteByDocument(ctx, id); err != nil {
		return err
	}

	if err := uc.docRepo.Delete(ctx, id); err != nil {
		return err
	}

	_ = uc.kbRepo.IncrementDocumentCount(ctx, doc.KnowledgeBaseID, -1)

	return nil
}
