package emailsending

import (
	"errors"
	"fmt"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

var (
	ErrPlanRequired   = errors.New("emailsending: an active paid plan is required to set up email")
	ErrPlanUnreadable = errors.New("emailsending: the workspace plan could not be read")
)

func SetupAllowedBy(active *workspace_plan.WorkspaceSubscription, err error) error {
	switch {
	case errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent),
		errors.Is(err, workspace_plan.ErrSubscriptionNotActive),
		errors.Is(err, workspace_plan.ErrSubscriptionNotFound):
		return ErrPlanRequired
	case err != nil:
		return fmt.Errorf("%w: %v", ErrPlanUnreadable, err)
	case active == nil:
		return ErrPlanRequired
	}
	return nil
}
