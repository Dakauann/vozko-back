package campaign

import (
	"errors"
	"strings"
	"time"
)

type LaunchMode string

const (
	LaunchNow      LaunchMode = "now"
	LaunchSchedule LaunchMode = "schedule"
	LaunchReady    LaunchMode = "ready"
)

var (
	ErrLaunchModeInvalid     = errors.New("campaign launch: the mode must be now, schedule or ready")
	ErrLaunchScheduleMissing = errors.New("campaign launch: scheduling needs a date and time")
	ErrSettingsNotApplied    = errors.New("campaign launch: the campaign was created but some settings were not applied, so it was not started")
)

type Launch struct {
	Mode LaunchMode
	At   time.Time
}

func ParseLaunch(mode string, at *time.Time) (Launch, error) {
	parsed := LaunchMode(strings.ToLower(strings.TrimSpace(mode)))
	switch parsed {
	case LaunchNow, LaunchReady:
		return Launch{Mode: parsed}, nil
	case LaunchSchedule:
		if at == nil || at.IsZero() {
			return Launch{}, ErrLaunchScheduleMissing
		}
		return Launch{Mode: parsed, At: at.UTC()}, nil
	default:
		return Launch{}, ErrLaunchModeInvalid
	}
}

func (l Launch) StartsNow() bool {
	return l.Mode == LaunchNow
}

func (m *Metrics) ReadyToStart(status Status, scheduled bool) bool {
	if m == nil || scheduled || NormalizeStatus(status) != StatusStopped {
		return false
	}
	return m.Pending > 0 && m.Sent == 0 && m.Delivered == 0 && m.Read == 0
}
