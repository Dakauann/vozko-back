package rag_usecase

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/rag"
)

type createDocumentUseCase struct {
	docRepo   rag.DocumentRepository
	kbRepo    rag.KnowledgeBaseRepository
	publishUC rag.PublishDocumentProcessingUseCase
}

func NewCreateDocumentUseCase(
	docRepo rag.DocumentRepository,
	kbRepo rag.KnowledgeBaseRepository,
	publishUC rag.PublishDocumentProcessingUseCase,
) rag.CreateDocumentUseCase {
	return &createDocumentUseCase{
		docRepo:   docRepo,
		kbRepo:    kbRepo,
		publishUC: publishUC,
	}
}

func (uc *createDocumentUseCase) Execute(ctx context.Context, input rag.CreateDocumentInput) (*rag.Document, error) {
	kb, err := uc.kbRepo.FindByID(ctx, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}

	if !kb.IsActive() {
		return nil, rag.ErrKnowledgeBaseNotFound
	}

	docCount, err := uc.docRepo.CountByKnowledgeBase(ctx, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if docCount >= rag.MaxDocumentsPerKnowledgeBase {
		return nil, rag.ErrMaxDocumentsReached
	}

	newSizeMB := float64(len(input.Content)) / (1024 * 1024)
	if kb.TotalSizeMB+newSizeMB > float64(rag.MaxTotalSizeMB) {
		return nil, rag.ErrMaxTotalSizeReached
	}

	doc := &rag.Document{
		ID:              uuid.New().String(),
		KnowledgeBaseID: input.KnowledgeBaseID,
		Name:            input.Name,
		Type:            input.Type,
		Status:          rag.DocumentStatusPending,
		Content:         input.Content,
		MediaID:         input.MediaID,
		SizeBytes:       int64(len(input.Content)),
		Metadata:        input.Metadata,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	doc.Normalize()

	if err := doc.Validate(); err != nil {
		return nil, err
	}

	if err := uc.docRepo.Create(ctx, doc); err != nil {
		return nil, err
	}

	_ = uc.kbRepo.IncrementDocumentCount(ctx, input.KnowledgeBaseID, 1)

	if uc.publishUC != nil {
		if err := uc.publishUC.Execute(ctx, doc.ID); err != nil {
			log.Printf("[RAG] warning: failed to publish processing job for document %s: %v", doc.ID, err)

		}
	}

	return doc, nil
}
