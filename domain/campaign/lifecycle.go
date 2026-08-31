// Package campaign holds what every campaign shares, on any channel.
//
// It exists because the product now runs campaigns over two transports that
// agree on almost everything an operator can see — the same four statuses, the
// same start/pause/stop verbs, the same metrics tiles, the same reset flow —
// and disagree only about how a message physically leaves the building.
//
// What lives here is the agreement. What does not live here is anything that
// names a transport: there is no template, no balance, no business phone and no
// instance in this package, and there must never be. A channel that needs one of
// those owns it in its own domain package and composes this one.
//
// The alternative was a second copy of the state machine and of the metrics
// algebra. Both are small enough to copy and important enough that a copy which
// drifts is a bug nobody notices until two screens disagree about what "enviadas"
// means.
package campaign

import (
	"errors"
	"strings"
)

// Status is the campaign lifecycle.
//
// Four values, closed set. There is deliberately no DRAFT: a campaign that has
// been created but never started is STOPPED, which is the same thing an operator
// sees after they stop one, and the pipeline treats the two identically. Adding
// DRAFT would introduce a state whose only difference is how it got there.
type Status string

const (
	StatusRunning   Status = "RUNNING"
	StatusPaused    Status = "PAUSED"
	StatusStopped   Status = "STOPPED"
	StatusCompleted Status = "COMPLETED"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusRunning, StatusPaused, StatusStopped, StatusCompleted:
		return true
	default:
		return false
	}
}

// NormalizeStatus trims and upper-cases a status read from the wire or the
// database, defaulting an empty one to STOPPED.
//
// STOPPED rather than RUNNING is the safe default and the reason this is a
// function rather than a cast: a row whose status column is somehow empty must
// not be interpreted as permission to start sending.
func NormalizeStatus(value Status) Status {
	trimmed := Status(strings.ToUpper(strings.TrimSpace(string(value))))
	if trimmed == "" {
		return StatusStopped
	}
	return trimmed
}

// Action is what an operator asked for.
type Action string

const (
	ActionStart Action = "START"
	ActionPause Action = "PAUSE"
	ActionStop  Action = "STOP"
)

// Lifecycle refusals.
//
// Distinguished rather than collapsed into one "invalid" because the HTTP layer
// maps each to a different status code and a different sentence, and because
// "already running" is a no-op an operator can ignore while "invalid status" is
// a bug they should report.
var (
	ErrAlreadyRunning = errors.New("campaign is already running")
	ErrNotRunning     = errors.New("campaign is not running")
	ErrAlreadyStopped = errors.New("campaign is already stopped")
	ErrStatusInvalid  = errors.New("campaign status is invalid")
	ErrActionInvalid  = errors.New("campaign requires a valid action")
)

// Transition is the answer to "may this action run from this status, and what
// does it move to".
//
// Returned as a value rather than applied, because applying it is a
// compare-and-swap the caller owns: the repository has to swap the status
// conditionally on the current one, and if the work that follows fails the
// caller has to swap it back. Deciding and applying in one call would hide the
// revert, which is exactly the part that must not be forgotten.
type Transition struct {
	Target Status
	// Revert is the status to restore if the work after the swap fails.
	Revert Status
	// FansOutWork reports whether this transition should enqueue pending
	// entries. Only START does. PAUSE and STOP must never enqueue, and spelling
	// that as a field rather than as a switch at the call site is what stops a
	// third caller getting it wrong.
	FansOutWork bool
}

// ResolveTransition validates an action against the current status.
//
// The table is the whole state machine, in one place, for every channel:
//
//	from \ action   START                PAUSE               STOP
//	RUNNING         ErrAlreadyRunning    -> PAUSED           -> STOPPED
//	PAUSED          -> RUNNING           ErrNotRunning       -> STOPPED
//	STOPPED         -> RUNNING           ErrNotRunning       ErrAlreadyStopped
//	COMPLETED       -> RUNNING           ErrNotRunning       -> STOPPED
//
// COMPLETED accepting START is deliberate: re-running a finished campaign after
// a reset is a normal thing to do, and refusing it would make reset a dead end.
func ResolveTransition(current Status, action Action) (Transition, error) {
	current = NormalizeStatus(current)
	if !current.IsValid() {
		return Transition{}, ErrStatusInvalid
	}

	switch action {
	case ActionStart:
		if current == StatusRunning {
			return Transition{}, ErrAlreadyRunning
		}
		return Transition{Target: StatusRunning, Revert: current, FansOutWork: true}, nil

	case ActionPause:
		if current != StatusRunning {
			return Transition{}, ErrNotRunning
		}
		return Transition{Target: StatusPaused, Revert: current}, nil

	case ActionStop:
		if current == StatusStopped {
			return Transition{}, ErrAlreadyStopped
		}
		return Transition{Target: StatusStopped, Revert: current}, nil

	default:
		return Transition{}, ErrActionInvalid
	}
}

// SwapFailure maps a lost compare-and-swap back onto the refusal that explains
// it.
//
// When UpdateStatus reports that it changed no row, another request won the race
// and the campaign is no longer in the status we read. The operator's remedy is
// the same as if we had seen that status in the first place, so the error must
// be too — otherwise the same click reports "already running" or "unexpected
// error" depending on timing.
func SwapFailure(action Action) error {
	switch action {
	case ActionStart:
		return ErrAlreadyRunning
	case ActionPause:
		return ErrNotRunning
	case ActionStop:
		return ErrAlreadyStopped
	default:
		return ErrStatusInvalid
	}
}
