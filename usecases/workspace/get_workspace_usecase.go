package workspace_usecase

import (
	"time"

	"vozko/domain/user"
	"vozko/domain/workspace"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type getWorkspaceUseCase struct {
	repo          workspace.Repository
	userRepo      user.UserRepository
	subscriptions workspace_plan.SubscriptionRepository
}

func NewGetWorkspaceUseCase(repo workspace.Repository, userRepo user.UserRepository, subscriptions workspace_plan.SubscriptionRepository) workspace.GetWorkspaceUseCase {
	return &getWorkspaceUseCase{repo: repo, userRepo: userRepo, subscriptions: subscriptions}
}

func (uc *getWorkspaceUseCase) Execute(userID, role, workspaceID string) (*workspace.Workspace, error) {
	var memberRole workspace.Role

	if role != "admin" {
		member := mustBeMember(uc.repo, workspaceID, userID)
		if member == nil {
			return nil, workspace.ErrUnauthorized
		}
		memberRole = member.Role
	}
	ws, err := uc.repo.GetWorkspaceByID(workspaceID)
	if err != nil {
		return nil, err
	}

	ws.CurrentUserRole = memberRole

	if ws.OwnerID != "" {
		owner, err := uc.userRepo.FindByID(ws.OwnerID)
		if err == nil && owner != nil {
			ws.OwnerName = owner.Username
			ws.OwnerEmail = owner.Email
		}
	}

	if uc.subscriptions != nil {
		sub, err := uc.subscriptions.GetCurrentByWorkspaceID(ws.ID, time.Now())
		if err == nil && sub != nil {
			ws.PlanName = sub.PlanName
			ws.SubscriptionStatus = string(sub.Status)
		}
	}

	return ws, nil
}
