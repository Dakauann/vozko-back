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

	// ErrDuplicate means an active memory with the same normalized content
	// already exists for this lead. Creation treats it as idempotent success;
	// an edit colliding with another row surfaces it to the caller.
	ErrDuplicate = errors.New("lead memory: an equivalent memory already exists")

	// ErrLimitReached means the lead is at MaxActiveMemoriesPerLead. The remedy
	// is curation: update or delete an existing memory, not a bigger cap.
	ErrLimitReached = errors.New("lead memory: memory limit reached for this lead")

	// ErrAmbiguousID means a memory-id prefix matched more than one row. The
	// caller should retry with more characters or the full id.
	ErrAmbiguousID = errors.New("lead memory: memory id prefix is ambiguous")
)
