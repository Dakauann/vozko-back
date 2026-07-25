package agent

import (
	"context"

	"vozko/domain/shared"
)

type CreateAgentUseCase interface {
	Execute(ctx context.Context, input CreateAgentInput) (*Agent, error)
}

type UpdateAgentUseCase interface {
	Execute(ctx context.Context, agentID string, input UpdateAgentInput) (*Agent, error)
}

type AssignDepartmentUseCase interface {
	Execute(ctx context.Context, agentID string) (*Agent, error)
}

type DeleteAgentUseCase interface {
	Execute(agentID string) error
}

type GetAgentUseCase interface {
	Execute(agentID string) (*Agent, error)
}

type ListAgentsUseCase interface {
	Execute(input ListAgentsInput) (*shared.PaginatedResult[*AgentListItem], error)
}
