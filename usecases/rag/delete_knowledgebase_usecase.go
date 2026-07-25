package rag_usecase

import (
	"context"

	"vozko/domain/rag"
)

type deleteKnowledgeBaseUseCase struct {
	kbRepo      rag.KnowledgeBaseRepository
	chunkRepo   rag.ChunkRepository
	vectorRepo  rag.VectorRepository
	agentKBRepo rag.AgentKnowledgeBaseRepository
}

func NewDeleteKnowledgeBaseUseCase(
	kbRepo rag.KnowledgeBaseRepository,
	chunkRepo rag.ChunkRepository,
	vectorRepo rag.VectorRepository,
	agentKBRepo rag.AgentKnowledgeBaseRepository,
) rag.DeleteKnowledgeBaseUseCase {
	return &deleteKnowledgeBaseUseCase{
		kbRepo:      kbRepo,
		chunkRepo:   chunkRepo,
		vectorRepo:  vectorRepo,
		agentKBRepo: agentKBRepo,
	}
}

func (uc *deleteKnowledgeBaseUseCase) Execute(ctx context.Context, id string) error {
	if _, err := uc.kbRepo.FindByID(ctx, id); err != nil {
		return err
	}

	if err := uc.vectorRepo.DeleteByKnowledgeBase(ctx, id); err != nil {
		return err
	}

	if err := uc.chunkRepo.DeleteByKnowledgeBase(ctx, id); err != nil {
		return err
	}

	agentIDs, _ := uc.agentKBRepo.FindByKnowledgeBase(ctx, id)
	for _, agentID := range agentIDs {
		_ = uc.agentKBRepo.Unlink(ctx, agentID, id)
	}

	return uc.kbRepo.Delete(ctx, id)
}
