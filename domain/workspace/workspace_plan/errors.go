package workspace_plan

import "errors"

var (
	ErrPlanNotFound              = errors.New("plan not found")
	ErrPlanArchived              = errors.New("plan is archived")
	ErrInvalidPlanName           = errors.New("plan name is required")
	ErrInvalidBasePrice          = errors.New("base price must be zero or greater")
	ErrInvalidMaxCallChannels    = errors.New("max call channels must be greater than zero")
	ErrSubscriptionNotFound      = errors.New("workspace subscription not found")
	ErrSubscriptionAlreadyExists = errors.New("workspace already has a current subscription")
	ErrSubscriptionNotCurrent    = errors.New("workspace subscription is not current")
	ErrSubscriptionCancelled     = errors.New("workspace subscription is already cancelled")
	ErrInvalidSubscription       = errors.New("invalid workspace subscription")
	ErrPlanNotVisible            = errors.New("plan is not available for this workspace")
	ErrDowngradeNotAllowed       = errors.New("cannot downgrade while subscription is active")
	ErrSubscriptionNotActive     = errors.New("workspace subscription is not active")

	ErrPlanExclusiveLocked = errors.New("plan is exclusive to an affiliate and cannot be assigned to workspaces")

	ErrExclusiveAffiliateRequired = errors.New("exclusive affiliate is required and must be active")

	ErrAffiliateRequired = errors.New("caller is not a registered affiliate")
)
