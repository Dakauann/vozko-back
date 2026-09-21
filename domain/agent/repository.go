package agent

import "vozko/domain/shared"

type Repository interface {
	Create(agent *Agent) error
	Update(agentID string, agent *Agent) error
	Delete(agentID string) error
	FindByID(agentID string) (*Agent, error)
	FindByIDs(agentIDs []string) ([]*Agent, error)

	List(input ListAgentsInput) (*shared.PaginatedResult[*AgentListItem], error)
}
