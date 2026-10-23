package emailsending

import (
	"errors"
	"testing"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

func TestSetupNeedsAnActivePaidPlan(t *testing.T) {
	active := &workspace_plan.WorkspaceSubscription{Status: workspace_plan.SubscriptionStatusActive}
	cases := []struct {
		name string
		sub  *workspace_plan.WorkspaceSubscription
		err  error
		want error
	}{
		{"an active plan", active, nil, nil},
		{"no plan was ever paid", nil, workspace_plan.ErrSubscriptionNotCurrent, ErrPlanRequired},
		{"the period ended unpaid", nil, workspace_plan.ErrSubscriptionNotCurrent, ErrPlanRequired},
		{"a cancelled plan", nil, workspace_plan.ErrSubscriptionNotActive, ErrPlanRequired},
		{"no subscription row", nil, workspace_plan.ErrSubscriptionNotFound, ErrPlanRequired},
		{"an empty answer", nil, nil, ErrPlanRequired},
		{"the plan cannot be read", nil, errors.New("connection refused"), ErrPlanUnreadable},
		{"a plan read alongside an error", active, errors.New("timeout"), ErrPlanUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SetupAllowedBy(tc.sub, tc.err)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v, want allowed", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
