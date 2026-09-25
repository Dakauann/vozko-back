package rag_usecase

import (
	"context"
	"encoding/base64"
	"log"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/rag"
)

type createDocumentUseCase struct {
	docRepo   rag.DocumentRepository
	kbRepo    rag.KnowledgeBaseRepository
	publishUC rag.PublishDocumentProcessingUseCase
	media     media.ReadMediaUseCase
}

func NewCreateDocumentUseCase(
	docRepo rag.DocumentRepository,
	kbRepo rag.KnowledgeBaseRepository,
	publishUC rag.PublishDocumentProcessingUseCase,
	media media.ReadMediaUseCase,
) rag.CreateDocumentUseCase {
	return &createDocumentUseCase{
		docRepo:   docRepo,
		kbRepo:    kbRepo,
		publishUC: publishUC,
		media:     media,
	}
}

func (uc *createDocumentUseCase) fromMedia(ctx context.Context, workspaceID string, input rag.CreateDocumentInput) (rag.CreateDocumentInput, error) {
	if uc.media == nil {
		return input, rag.ErrDocumentContentRequired
	}
	content, err := uc.media.Read(ctx, workspaceID, input.MediaID)
	if err != nil {
		return input, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = content.Name
	}
	if path.Ext(name) == "" {
		name += path.Ext(content.Name)
	}
	metadata := make(map[string]string, len(input.Metadata)+1)
	for k, v := range input.Metadata {
		metadata[k] = v
	}
	metadata[rag.MetadataEncoding] = rag.EncodingBase64
	input.Name = name
	if !input.Type.IsValid() {
		input.Type = rag.DocumentTypeFromName(name)
	}
	input.Content = base64.StdEncoding.EncodeToString(content.Data)
	input.Metadata = metadata
	return input, nil
}

func (uc *createDocumentUseCase) Execute(ctx context.Context, input rag.CreateDocumentInput) (*rag.Document, error) {
	kb, err := uc.kbRepo.FindByID(ctx, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}

	if !kb.IsActive() {
		return nil, rag.ErrKnowledgeBaseNotFound
	}
	if strings.TrimSpace(input.MediaID) != "" && strings.TrimSpace(input.Content) == "" {
		if input, err = uc.fromMedia(ctx, kb.WorkspaceID, input); err != nil {
			return nil, err
		}
	}

	docCount, err := uc.docRepo.CountByKnowledgeBase(ctx, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if docCount >= rag.MaxDocumentsPerKnowledgeBase {
		return nil, rag.ErrMaxDocumentsReached
	}

	size := rag.ContentSize(input.Content, input.Metadata)
	if kb.TotalSizeMB+rag.SizeInMB(size) > float64(rag.MaxTotalSizeMB) {
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
		SizeBytes:       size,
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
	if err := uc.kbRepo.AddTotalSize(ctx, input.KnowledgeBaseID, size); err != nil {
		log.Printf("[RAG] warning: failed to add document %s size to knowledge base %s: %v", doc.ID, input.KnowledgeBaseID, err)
	}

	if uc.publishUC != nil {
		if err := uc.publishUC.Execute(ctx, doc.ID); err != nil {
			log.Printf("[RAG] warning: failed to publish processing job for document %s: %v", doc.ID, err)

		}
	}

	return doc, nil
}
