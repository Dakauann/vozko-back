package rag_usecase

import (
	"context"

	"vozko/domain/rag"
)

type linkAgentKnowledgeBasesUseCase struct {
	agentKBRepo rag.AgentKnowledgeBaseRepository
	kbRepo      rag.KnowledgeBaseRepository
}

func NewLinkAgentKnowledgeBasesUseCase(
	agentKBRepo rag.AgentKnowledgeBaseRepository,
	kbRepo rag.KnowledgeBaseRepository,
) rag.LinkAgentKnowledgeBasesUseCase {
	return &linkAgentKnowledgeBasesUseCase{
		agentKBRepo: agentKBRepo,
		kbRepo:      kbRepo,
	}
}

func (uc *linkAgentKnowledgeBasesUseCase) Execute(ctx context.Context, input rag.LinkAgentKnowledgeBasesInput) error {
	for _, kbID := range input.KnowledgeBaseIDs {
		if _, err := uc.kbRepo.FindByID(ctx, kbID); err != nil {
			return err
		}
	}

	return uc.agentKBRepo.ReplaceForAgent(ctx, input.AgentID, input.KnowledgeBaseIDs)
}

type getAgentKnowledgeBasesUseCase struct {
	agentKBRepo rag.AgentKnowledgeBaseRepository
	kbRepo      rag.KnowledgeBaseRepository
}

func NewGetAgentKnowledgeBasesUseCase(
	agentKBRepo rag.AgentKnowledgeBaseRepository,
	kbRepo rag.KnowledgeBaseRepository,
) rag.GetAgentKnowledgeBasesUseCase {
	return &getAgentKnowledgeBasesUseCase{
		agentKBRepo: agentKBRepo,
		kbRepo:      kbRepo,
	}
}

func (uc *getAgentKnowledgeBasesUseCase) Execute(ctx context.Context, agentID string) ([]*rag.KnowledgeBase, error) {
	kbIDs, err := uc.agentKBRepo.FindByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if len(kbIDs) == 0 {
		return []*rag.KnowledgeBase{}, nil
	}

	return uc.kbRepo.FindByIDs(ctx, kbIDs)
}
