package emailcampaign

import (
	"errors"
	"slices"

	"vozko/domain/campaign"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusReady     Status = "ready"
	StatusScheduled Status = "scheduled"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusHeld      Status = "held"
	StatusCompleted Status = "completed"
	StatusStopped   Status = "stopped"
)

func AllStatuses() []Status {
	return []Status{StatusDraft, StatusReady, StatusScheduled, StatusRunning, StatusPaused, StatusHeld, StatusCompleted, StatusStopped}
}

func (s Status) Valid() bool {
	for _, known := range AllStatuses() {
		if s == known {
			return true
		}
	}
	return false
}

func EditableStatuses() []Status {
	return []Status{StatusDraft, StatusReady, StatusScheduled}
}

func DeletableStatuses() []Status {
	return []Status{StatusDraft, StatusReady}
}

func (s Status) Editable() bool {
	return slices.Contains(EditableStatuses(), s)
}

func (s Status) Deletable() bool {
	return slices.Contains(DeletableStatuses(), s)
}

func (s Status) AfterEdit() Status {
	if s == StatusReady {
		return StatusDraft
	}
	return s
}

func (s Status) Launched() bool {
	switch s {
	case StatusRunning, StatusPaused, StatusHeld, StatusCompleted, StatusStopped:
		return true
	}
	return false
}

func (s Status) Sending() bool {
	return s == StatusRunning || s == StatusHeld
}

func (s Status) Finished() bool {
	return s == StatusCompleted || s == StatusStopped
}

type Action string

const (
	ActionReady      Action = "ready"
	ActionSchedule   Action = "schedule"
	ActionUnschedule Action = "unschedule"
	ActionStart      Action = "start"
	ActionPause      Action = "pause"
	ActionHold       Action = "hold"
	ActionStop       Action = "stop"
	ActionComplete   Action = "complete"
)

func (a Action) known() bool {
	switch a {
	case ActionReady, ActionSchedule, ActionUnschedule, ActionStart, ActionPause, ActionHold, ActionStop, ActionComplete:
		return true
	}
	return false
}

var (
	ErrStatusInvalid = errors.New("emailcampaign: unknown campaign status")
	ErrFinished      = errors.New("emailcampaign: the campaign already finished")
	ErrNotDraft      = errors.New("emailcampaign: only a draft can do this")
	ErrNotScheduled  = errors.New("emailcampaign: the campaign is not scheduled")
	ErrNotLaunched   = errors.New("emailcampaign: the campaign was never launched")
	ErrAlreadyHeld   = errors.New("emailcampaign: the campaign is already held")
)

func Transition(from Status, action Action) (Status, error) {
	if !from.Valid() {
		return "", ErrStatusInvalid
	}
	if !action.known() {
		return "", campaign.ErrActionInvalid
	}
	if from.Finished() {
		return "", ErrFinished
	}
	switch action {
	case ActionReady:
		if from != StatusDraft {
			return "", ErrNotDraft
		}
		return StatusReady, nil
	case ActionSchedule:
		if from != StatusDraft && from != StatusReady && from != StatusScheduled {
			return "", ErrNotDraft
		}
		return StatusScheduled, nil
	case ActionUnschedule:
		if from != StatusScheduled {
			return "", ErrNotScheduled
		}
		return StatusDraft, nil
	case ActionStart:
		return start(from)
	case ActionPause:
		return core(from, campaign.ActionPause, StatusPaused)
	case ActionHold:
		if from == StatusHeld {
			return "", ErrAlreadyHeld
		}
		return core(from, campaign.ActionPause, StatusHeld)
	case ActionStop:
		return stop(from)
	default:
		if from != StatusRunning {
			return "", campaign.ErrNotRunning
		}
		return StatusCompleted, nil
	}
}

func start(from Status) (Status, error) {
	switch from {
	case StatusDraft, StatusReady, StatusScheduled, StatusHeld:
		return StatusRunning, nil
	}
	return core(from, campaign.ActionStart, StatusRunning)
}

func stop(from Status) (Status, error) {
	switch from {
	case StatusDraft, StatusReady:
		return "", ErrNotLaunched
	case StatusScheduled:
		return StatusStopped, nil
	}
	return core(from, campaign.ActionStop, StatusStopped)
}

func core(from Status, action campaign.Action, target Status) (Status, error) {
	current, launched := coreStatus(from)
	if !launched {
		return "", campaign.ErrNotRunning
	}
	if _, err := campaign.ResolveTransition(current, action); err != nil {
		return "", err
	}
	return target, nil
}

func coreStatus(s Status) (campaign.Status, bool) {
	switch s {
	case StatusRunning, StatusHeld:
		return campaign.StatusRunning, true
	case StatusPaused:
		return campaign.StatusPaused, true
	}
	return "", false
}

func LiveStatuses() []Status {
	return []Status{StatusScheduled, StatusRunning, StatusHeld}
}

func (s Status) KeepsSenderAwake() bool {
	for _, live := range LiveStatuses() {
		if s == live {
			return true
		}
	}
	return false
}

func AccountKeepingStatuses() []Status {
	return append(LiveStatuses(), StatusReady, StatusPaused)
}
