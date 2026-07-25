package workspace_addon_usecase

import (
	workspace_addon "vozko/domain/workspace/workspace_addon"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type cancelAddonSubscriptionUseCase struct {
	subs workspace_addon.AddonSubscriptionRepository
	now  clockFn
}

func NewCancelAddonSubscriptionUseCase(subs workspace_addon.AddonSubscriptionRepository) workspace_addon.CancelAddonSubscriptionUseCase {
	return &cancelAddonSubscriptionUseCase{subs: subs, now: utcNow}
}

func (uc *cancelAddonSubscriptionUseCase) Execute(workspaceID, subscriptionID string) (*workspace_addon.AddonSubscription, error) {
	sub, err := uc.subs.GetByID(subscriptionID)
	if err != nil {
		return nil, err
	}
	if sub.WorkspaceID != workspaceID {
		return nil, workspace_addon.ErrAddonSubscriptionNotFound
	}
	if sub.Status != workspace_plan.SubscriptionStatusActive {
		return sub, nil
	}
	now := uc.now()
	sub.CancelledAt = &now
	sub.UpdatedAt = now
	if err := uc.subs.Update(sub); err != nil {
		return nil, err
	}
	return sub, nil
}
