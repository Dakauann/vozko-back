package callrouting

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultRingSeconds    = 15
	defaultMaxWaitSeconds = 300
	defaultWrapUpSeconds  = 10
	minRingSeconds        = 5
	maxRingSeconds        = 60
	minMaxWaitSeconds     = 10
	maxMaxWaitSeconds     = 3600
	maxWrapUpSeconds      = 300
	maxQueueNameRunes     = 80
)

type Queue struct {
	ID             string
	WorkspaceID    string
	Name           string
	Strategy       Strategy
	DepartmentID   string
	MemberUserIDs  []string
	RingSeconds    int
	MaxWaitSeconds int
	WrapUpSeconds  int
	HoldMusic      HoldMusicRef
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (q *Queue) ApplyDefaults() {
	if q.Strategy == "" {
		q.Strategy = StrategyLongestIdle
	}
	if q.RingSeconds == 0 {
		q.RingSeconds = defaultRingSeconds
	}
	if q.MaxWaitSeconds == 0 {
		q.MaxWaitSeconds = defaultMaxWaitSeconds
	}
	if q.WrapUpSeconds == 0 {
		q.WrapUpSeconds = defaultWrapUpSeconds
	}
	if q.HoldMusic.IsZero() {
		q.HoldMusic = HoldMusicRef{PresetID: DefaultHoldPreset}
	}
}

func (q *Queue) Validate() error {
	q.Name = strings.TrimSpace(q.Name)
	q.DepartmentID = strings.TrimSpace(q.DepartmentID)
	q.MemberUserIDs = uniqueIDs(q.MemberUserIDs)
	switch {
	case strings.TrimSpace(q.WorkspaceID) == "":
		return ErrWorkspaceRequired
	case q.Name == "" || utf8.RuneCountInString(q.Name) > maxQueueNameRunes:
		return ErrQueueNameRequired
	case q.DepartmentID == "" && len(q.MemberUserIDs) == 0:
		return ErrQueueMembersRequired
	case q.DepartmentID != "" && len(q.MemberUserIDs) > 0:
		return ErrQueueMembersAmbiguous
	case !q.Strategy.Valid():
		return fmt.Errorf("%w: %q", ErrInvalidStrategy, q.Strategy)
	case !within(q.RingSeconds, minRingSeconds, maxRingSeconds),
		!within(q.MaxWaitSeconds, minMaxWaitSeconds, maxMaxWaitSeconds),
		!within(q.WrapUpSeconds, 0, maxWrapUpSeconds):
		return ErrQueueTimingOutOfRange
	}
	return q.HoldMusic.Validate()
}

func (q Queue) RingTimeout() time.Duration { return time.Duration(q.RingSeconds) * time.Second }

func (q Queue) MaxWait() time.Duration { return time.Duration(q.MaxWaitSeconds) * time.Second }

func (q Queue) WrapUp() time.Duration { return time.Duration(q.WrapUpSeconds) * time.Second }

func within(value, low, high int) bool {
	return value >= low && value <= high
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
