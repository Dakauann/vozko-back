package audience

import "errors"

var (
	ErrNotFound          = errors.New("comment analysis: not found")
	ErrWorkspaceRequired = errors.New("comment analysis: workspace id is required")
	ErrSubjectIDRequired = errors.New("comment analysis: source comment id is required")
	ErrContainerInvalid  = errors.New("comment analysis: container reference is invalid")

	ErrStatusTransition = errors.New("comment analysis: invalid status transition")

	ErrInvalidClassification = errors.New("comment analysis: classification has a value outside the rubric")

	ErrInvalidFilter = errors.New("comment analysis: invalid filter")

	ErrTooManyTopics     = errors.New("comment analysis: too many topics")
	ErrTopicKeyInvalid   = errors.New("comment analysis: topic key must be normalised")
	ErrTopicLabelTooLong = errors.New("comment analysis: topic label is too long")

	ErrBudgetInvalid = errors.New("comment analysis: budget is invalid")

	ErrChannelUnavailable = errors.New("comment analysis: the alert channel has no number to send from")

	ErrCycleCapReached   = errors.New("comment analysis: per-cycle token ceiling reached")
	ErrDailyCapReached   = errors.New("comment analysis: daily comment cap reached")
	ErrBalanceBelowFloor = errors.New("comment analysis: workspace balance is below the floor")
)
