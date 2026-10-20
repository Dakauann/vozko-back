package notification

import (
	"strconv"
	"time"
)

type EpisodeStep int

const (
	FirstNotice EpisodeStep = 1
	Reminder    EpisodeStep = 2
	LastNotice  EpisodeStep = 3

	EpisodeSteps = int(LastNotice)
)

const EpisodeQuietClose = 48 * time.Hour

var waitBeforeStep = [EpisodeSteps]time.Duration{
	0,
	24 * time.Hour,
	72 * time.Hour,
}

type EpisodeClose string

const (
	EpisodeResolved  EpisodeClose = "resolved"
	EpisodeWentQuiet EpisodeClose = "quiet"
)

type Episode struct {
	ID             string
	WorkspaceID    string
	Key            string
	StepsSent      EpisodeStep
	OpenedAt       time.Time
	LastSeenAt     time.Time
	LastSentAt     *time.Time
	NotificationID string
	ClosedAt       *time.Time
	CloseReason    EpisodeClose
}

func NewEpisode(workspaceID, key string, at time.Time) Episode {
	return Episode{WorkspaceID: workspaceID, Key: key, OpenedAt: at, LastSeenAt: at}
}

func (e Episode) Quiet(now time.Time) bool {
	return !now.Before(e.LastSeenAt.Add(EpisodeQuietClose))
}

func (e Episode) NextStep(now time.Time) (EpisodeStep, bool) {
	if int(e.StepsSent) >= EpisodeSteps {
		return 0, false
	}
	next := e.StepsSent + 1
	if e.StepsSent == 0 || e.LastSentAt == nil {
		return next, true
	}
	if now.Before(e.LastSentAt.Add(waitBeforeStep[next-1])) {
		return 0, false
	}
	return next, true
}

func (e Episode) StepKey(step EpisodeStep) string {
	return "episode:" + e.ID + ":" + strconv.Itoa(int(step))
}

func EpisodeKeys(keys []string) []string {
	return uniqueTrimmed(keys)
}
