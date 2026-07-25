package workspace_usecase

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/workspace"
)

type createWorkspaceUseCase struct {
	repo workspace.Repository
}

func NewCreateWorkspaceUseCase(repo workspace.Repository) workspace.CreateWorkspaceUseCase {
	return &createWorkspaceUseCase{repo: repo}
}

const maxWorkspacesPerUser = 10

func (uc *createWorkspaceUseCase) Execute(ownerID, name string) (*workspace.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, workspace.ErrWorkspaceNameRequired
	}

	existing, err := uc.repo.ListWorkspacesByUser(ownerID, "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace count: %w", err)
	}
	if len(existing) >= maxWorkspacesPerUser {
		return nil, workspace.ErrWorkspaceLimitReached
	}

	ws := &workspace.Workspace{
		ID:      uuid.New().String(),
		OwnerID: ownerID,
		Name:    name,
	}
	if err := uc.repo.CreateWorkspace(ws); err != nil {
		return nil, err
	}

	member := &workspace.Member{
		ID:          uuid.New().String(),
		WorkspaceID: ws.ID,
		UserID:      ownerID,
		Role:        workspace.RoleOwner,
	}
	if err := uc.repo.AddMember(member); err != nil {
		return nil, err
	}

	return ws, nil
}
