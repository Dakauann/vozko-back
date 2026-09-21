package campaign

import (
	"errors"
	"strings"
)

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

func NormalizeStatus(value Status) Status {
	trimmed := Status(strings.ToUpper(strings.TrimSpace(string(value))))
	if trimmed == "" {
		return StatusStopped
	}
	return trimmed
}

type Action string

const (
	ActionStart Action = "START"
	ActionPause Action = "PAUSE"
	ActionStop  Action = "STOP"
)

var (
	ErrAlreadyRunning = errors.New("campaign is already running")
	ErrNotRunning     = errors.New("campaign is not running")
	ErrAlreadyStopped = errors.New("campaign is already stopped")
	ErrStatusInvalid  = errors.New("campaign status is invalid")
	ErrActionInvalid  = errors.New("campaign requires a valid action")
)

type Transition struct {
	Target      Status
	Revert      Status
	FansOutWork bool
}

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
