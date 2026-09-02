package comment_analysis

import "errors"

var (
	ErrNotFound                = errors.New("comment analysis: not found")
	ErrWorkspaceRequired       = errors.New("comment analysis: workspace id is required")
	ErrSourceCommentIDRequired = errors.New("comment analysis: source comment id is required")
	ErrContainerInvalid        = errors.New("comment analysis: container reference is invalid")

	// ErrStatusTransition is returned for any illegal move in the status
	// machine. It is the domain's half of the at-most-once contract: the other
	// half is the repository's conditional write.
	ErrStatusTransition = errors.New("comment analysis: invalid status transition")

	// ErrInvalidClassification means a model result carried a label outside
	// the rubric. It is counted, never applied to a row.
	ErrInvalidClassification = errors.New("comment analysis: classification has a value outside the rubric")

	// ErrInvalidFilter covers every malformed list/stats/trend input; the
	// wrapped message names the field.
	ErrInvalidFilter = errors.New("comment analysis: invalid filter")

	ErrTooManyTopics     = errors.New("comment analysis: too many topics")
	ErrTopicKeyInvalid   = errors.New("comment analysis: topic key must be normalised")
	ErrTopicLabelTooLong = errors.New("comment analysis: topic label is too long")

	ErrBudgetInvalid = errors.New("comment analysis: budget is invalid")

	// The three ways a cycle stops early. Each is a distinct error because the
	// operator has to be told something different: the first two resolve on
	// their own, the third needs money.
	ErrCycleCapReached   = errors.New("comment analysis: per-cycle token ceiling reached")
	ErrDailyCapReached   = errors.New("comment analysis: daily comment cap reached")
	ErrBalanceBelowFloor = errors.New("comment analysis: workspace balance is below the floor")
)
