package rag_usecase

import (
	"context"

	"vozko/domain/rag"
)

type ragService struct {
	queryUseCase      rag.QueryKnowledgeBaseUseCase
	agentQueryUseCase rag.QueryAgentKnowledgeBaseUseCase
}

func NewRAGService(
	queryUseCase rag.QueryKnowledgeBaseUseCase,
	agentQueryUseCase rag.QueryAgentKnowledgeBaseUseCase,
) rag.RAGService {
	return &ragService{
		queryUseCase:      queryUseCase,
		agentQueryUseCase: agentQueryUseCase,
	}
}

func (s *ragService) Query(ctx context.Context, input rag.QueryInput) (*rag.QueryOutput, error) {
	return s.queryUseCase.Execute(ctx, input)
}

func (s *ragService) QueryForAgent(ctx context.Context, input rag.AgentQueryInput) (*rag.QueryOutput, error) {
	return s.agentQueryUseCase.Execute(ctx, input)
}
