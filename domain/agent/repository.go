package agent

import "vozko/domain/shared"

type Repository interface {
	Create(agent *Agent) error
	Update(agentID string, agent *Agent) error
	Delete(agentID string) error
	FindByID(agentID string) (*Agent, error)
	// FindByIDs batch-loads agents by id (order not guaranteed, missing ids skipped).
	// Used by inbox enrichment to name the AI handler without an N+1.
	FindByIDs(agentIDs []string) ([]*Agent, error)

	List(input ListAgentsInput) (*shared.PaginatedResult[*AgentListItem], error)
}
