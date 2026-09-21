package scheduled_message_usecase

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
)

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

func (s *windowService) State(entryID, entryType string) sm.WindowState {
	return s.stateFrom(s.windows.GetWindowStatusForEntry(entryID, entryType))
}

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

func (s *windowService) IsOpen(entryID, entryType string) bool {
	return s.windows.GetWindowStatusForEntry(entryID, entryType).Open
}

func (s *windowService) Validate(entryID, entryType string, at time.Time) (sm.WindowState, error) {
	live := s.windows.GetWindowStatusForEntry(entryID, entryType)
	return s.stateFrom(live), sm.ValidateScheduledAt(at, live.Open, live.ExpiresAt, s.clock.Now())
}
