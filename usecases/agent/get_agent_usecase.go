package agent_usecase

import "vozko/domain/agent"

type getAgentUseCase struct {
	repo agent.Repository
}

func NewGetAgentUseCase(repo agent.Repository) agent.GetAgentUseCase {
	return &getAgentUseCase{repo: repo}
}

func (uc *getAgentUseCase) Execute(agentID string) (*agent.Agent, error) {
	if agentID == "" {
		return nil, agent.ErrAgentNotFound
	}

	return uc.repo.FindByID(agentID)
}
