package carriercampaign

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
	StatusCompleted Status = "completed"
	StatusStopped   Status = "stopped"
)

func AllStatuses() []Status {
	return []Status{StatusDraft, StatusReady, StatusScheduled, StatusRunning, StatusPaused, StatusCompleted, StatusStopped}
}

func EditableStatuses() []Status {
	return []Status{StatusDraft, StatusReady, StatusScheduled}
}

func DeletableStatuses() []Status {
	return []Status{StatusDraft, StatusReady}
}

func (s Status) Valid() bool {
	for _, known := range AllStatuses() {
		if s == known {
			return true
		}
	}
	return false
}

func (s Status) Editable() bool {
	return slices.Contains(EditableStatuses(), s)
}

func (s Status) Deletable() bool {
	return slices.Contains(DeletableStatuses(), s)
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
	ActionStop       Action = "stop"
	ActionComplete   Action = "complete"
	ActionRecur      Action = "recur"
)

func (a Action) known() bool {
	switch a {
	case ActionReady, ActionSchedule, ActionUnschedule, ActionStart, ActionPause, ActionStop, ActionComplete, ActionRecur:
		return true
	}
	return false
}

var (
	ErrStatusInvalid  = errors.New("carriercampaign: unknown campaign status")
	ErrFinished       = errors.New("carriercampaign: the campaign already finished")
	ErrNotEditable    = errors.New("carriercampaign: only drafts, ready and scheduled campaigns can be changed")
	ErrNotDraft       = errors.New("carriercampaign: only a draft can do this")
	ErrReadyScheduled = errors.New("carriercampaign: a ready campaign starts when the person wants, so it carries no schedule")
	ErrNotScheduled   = errors.New("carriercampaign: the campaign is not scheduled")
	ErrNotLaunched    = errors.New("carriercampaign: the campaign was never launched")
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
		if !from.Editable() {
			return "", ErrNotEditable
		}
		return StatusScheduled, nil
	case ActionUnschedule:
		if from != StatusScheduled {
			return "", ErrNotScheduled
		}
		return StatusDraft, nil
	case ActionStart:
		if from.Editable() {
			return StatusRunning, nil
		}
		return core(from, campaign.ActionStart, StatusRunning)
	case ActionPause:
		return core(from, campaign.ActionPause, StatusPaused)
	case ActionStop:
		switch from {
		case StatusDraft, StatusReady:
			return "", ErrNotLaunched
		case StatusScheduled:
			return StatusStopped, nil
		}
		return core(from, campaign.ActionStop, StatusStopped)
	case ActionComplete:
		return finishRun(from, StatusCompleted)
	default:
		return finishRun(from, StatusScheduled)
	}
}

func finishRun(from, target Status) (Status, error) {
	if from != StatusRunning {
		return "", campaign.ErrNotRunning
	}
	return target, nil
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
	case StatusRunning:
		return campaign.StatusRunning, true
	case StatusPaused:
		return campaign.StatusPaused, true
	}
	return "", false
}
