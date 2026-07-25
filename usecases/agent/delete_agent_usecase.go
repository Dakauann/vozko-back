package agent_usecase

import "vozko/domain/agent"

type deleteAgentUseCase struct {
	repo agent.Repository
}

func NewDeleteAgentUseCase(repo agent.Repository) agent.DeleteAgentUseCase {
	return &deleteAgentUseCase{repo: repo}
}

func (uc *deleteAgentUseCase) Execute(agentID string) error {
	if agentID == "" {
		return agent.ErrAgentNotFound
	}
	return uc.repo.Delete(agentID)
}
