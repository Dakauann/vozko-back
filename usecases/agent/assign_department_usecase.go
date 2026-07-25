package agent_usecase

import (
	"context"

	"vozko/domain/agent"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type assignDepartmentUseCase struct {
	repo               agent.Repository
	departmentResolver workspace_department.CreationDepartmentResolver
}

func NewAssignDepartmentUseCase(
	repo agent.Repository,
	departmentResolver workspace_department.CreationDepartmentResolver,
) agent.AssignDepartmentUseCase {
	return &assignDepartmentUseCase{
		repo:               repo,
		departmentResolver: departmentResolver,
	}
}

func (uc *assignDepartmentUseCase) Execute(ctx context.Context, agentID string) (*agent.Agent, error) {
	if agentID == "" {
		return nil, agent.ErrAgentNotFound
	}

	current, err := uc.repo.FindByID(agentID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, agent.ErrAgentNotFound
	}

	departmentID, err := uc.departmentResolver.Resolve(ctx, current.WorkspaceID)
	if err != nil {
		return nil, err
	}

	current.DepartmentID = departmentID
	if err := uc.repo.Update(agentID, current); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(agentID)
}
