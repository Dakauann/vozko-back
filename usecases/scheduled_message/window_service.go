package scheduled_message_usecase

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
)

// windowService answers "can this conversation be scheduled into, and until
// when" for every channel.
//
// It is one object rather than a helper on each use case because three of them
// need the same answer — schedule, reschedule and list — and a second copy of
// the rule is a second place for the client's date picker and the server's
// validation to drift apart.
type windowService struct {
	windows sm.WindowReader
	clock   sm.Clock
}

func newWindowService(windows sm.WindowReader, clock sm.Clock) (*windowService, error) {
	missing := []string{}
	if windows == nil {
		missing = append(missing, "window reader")
	}
	if clock == nil {
		missing = append(missing, "clock")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("scheduled message: missing %s", strings.Join(missing, ", "))
	}
	return &windowService{windows: windows, clock: clock}, nil
}

// State reports the conversation's window and the latest time it will accept.
//
// LatestAllowedAt is nil for a closed window rather than zero: there is no such
// time, and a zero timestamp rendered by a client is a date in year 1.
func (s *windowService) State(entryID, entryType string) sm.WindowState {
	return s.stateFrom(s.windows.GetWindowStatusForEntry(entryID, entryType))
}

// stateFrom is the single translation from the shared conversation rule to the
// scheduler's own shape, so the two cannot drift on what "open" or "closed
// because X" means.
func (s *windowService) stateFrom(live conversation.WindowState) sm.WindowState {
	state := sm.WindowState{
		Open:         live.Open,
		ExpiresAt:    live.ExpiresAt,
		ClosedReason: string(live.Reason),
	}
	if latest, err := sm.LatestAllowed(live.Open, live.ExpiresAt, s.clock.Now()); err == nil {
		state.LatestAllowedAt = &latest
	}
	return state
}

// IsOpen reports only whether the conversation can be replied to right now.
func (s *windowService) IsOpen(entryID, entryType string) bool {
	return s.windows.GetWindowStatusForEntry(entryID, entryType).Open
}

// Validate checks a chosen time against the live window.
//
// The state is returned alongside the error, not instead of it, so a caller can
// tell the operator both what was refused and what the boundary actually is.
// A refusal that does not name the boundary makes the next attempt a guess.
func (s *windowService) Validate(entryID, entryType string, at time.Time) (sm.WindowState, error) {
	live := s.windows.GetWindowStatusForEntry(entryID, entryType)
	return s.stateFrom(live), sm.ValidateScheduledAt(at, live.Open, live.ExpiresAt, s.clock.Now())
}
