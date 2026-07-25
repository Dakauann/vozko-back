package balance_usecase

import (
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

// allowAllSubscriptionChecker is a shared test helper that stands in for the
// subscription checker dependency. It was originally defined in the now-removed
// consume_sms_usecase_test.go; it is recovered here because non-SMS tests
// (e.g. the WhatsApp billing tests) still depend on it.
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
