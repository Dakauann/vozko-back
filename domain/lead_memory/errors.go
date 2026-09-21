package lead_memory

import "errors"

var (
	ErrNotFound          = errors.New("lead memory: not found")
	ErrWorkspaceRequired = errors.New("lead memory: workspace id is required")
	ErrLeadRequired      = errors.New("lead memory: lead id is required")
	ErrContentRequired   = errors.New("lead memory: content is required")
	ErrContentTooLong    = errors.New("lead memory: content is too long")
	ErrInvalidCategory   = errors.New("lead memory: category is invalid")
	ErrActorRequired     = errors.New("lead memory: actor is required")

	ErrDuplicate = errors.New("lead memory: an equivalent memory already exists")

	ErrLimitReached = errors.New("lead memory: memory limit reached for this lead")

	ErrAmbiguousID = errors.New("lead memory: memory id prefix is ambiguous")
)
