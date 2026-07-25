package agent_usecase

import (
	"vozko/domain/agent"
	"vozko/domain/shared"
)

type listAgentsUseCase struct {
	repo agent.Repository
}

func NewListAgentsUseCase(repo agent.Repository) agent.ListAgentsUseCase {
	return &listAgentsUseCase{repo: repo}
}

func (uc *listAgentsUseCase) Execute(input agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	return uc.repo.List(input)
}
