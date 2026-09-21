package balance_usecase

import (
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

type allowAllSubscriptionChecker struct {
	err error
}

func (c *allowAllSubscriptionChecker) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscription, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &workspace_plan.WorkspaceSubscription{
		WorkspaceID:      workspaceID,
		PlanDefinitionID: "plan-1",
		PlanName:         "Starter",
		MaxCallChannels:  3,
		Status:           workspace_plan.SubscriptionStatusActive,
	}, nil
}
