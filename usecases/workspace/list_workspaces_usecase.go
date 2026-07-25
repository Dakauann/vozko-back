package workspace_usecase

import (
	"time"

	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/domain/workspace"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type listWorkspacesUseCase struct {
	repo          workspace.Repository
	userRepo      user.UserRepository
	subscriptions workspace_plan.SubscriptionRepository
}

func NewListWorkspacesUseCase(repo workspace.Repository, userRepo user.UserRepository, subscriptions workspace_plan.SubscriptionRepository) workspace.ListWorkspacesUseCase {
	return &listWorkspacesUseCase{repo: repo, userRepo: userRepo, subscriptions: subscriptions}
}

func (uc *listWorkspacesUseCase) Execute(userID, role, search, memberEmail string, page, pageSize int) (interface{}, error) {
	if role == "admin" {
		if pageSize <= 0 {
			pageSize = shared.DefaultPageSize
		}
		if page <= 0 {
			page = 1
		}
		items, total, err := uc.repo.ListAllWorkspaces(search, memberEmail, page, pageSize)
		if err != nil {
			return nil, err
		}

		if len(items) > 0 {
			ids := make([]string, len(items))
			for i, ws := range items {
				ids[i] = ws.ID
			}

			counts, err := uc.repo.CountMembersByWorkspaceIDs(ids)
			if err == nil {
				for _, ws := range items {
					ws.MemberCount = counts[ws.ID]
				}
			}

			ownerIDSet := make(map[string]struct{})
			for _, ws := range items {
				if ws.OwnerID != "" {
					ownerIDSet[ws.OwnerID] = struct{}{}
				}
			}
			if len(ownerIDSet) > 0 {
				ownerIDs := make([]string, 0, len(ownerIDSet))
				for id := range ownerIDSet {
					ownerIDs = append(ownerIDs, id)
				}
				users, err := uc.userRepo.FindByIDs(ownerIDs)
				if err == nil {
					userMap := make(map[string]*user.User, len(users))
					for _, u := range users {
						userMap[u.ID] = u
					}
					for _, ws := range items {
						if u, ok := userMap[ws.OwnerID]; ok {
							ws.OwnerName = u.Username
							ws.OwnerEmail = u.Email
						}
					}
				}
			}

			if uc.subscriptions != nil {
				subs, err := uc.subscriptions.GetCurrentByWorkspaceIDs(ids, time.Now())
				if err == nil {
					for _, ws := range items {
						if sub, ok := subs[ws.ID]; ok {
							ws.PlanName = sub.PlanName
							ws.SubscriptionStatus = string(sub.Status)
						}
					}
				}
			}
		}

		pagination := shared.Pagination{Page: page, PageSize: pageSize}
		return shared.NewPaginatedResult(items, pagination, total), nil
	}

	workspaces, err := uc.repo.ListWorkspacesByUser(userID, search, memberEmail)
	if err != nil {
		return nil, err
	}

	return workspaces, nil
}
