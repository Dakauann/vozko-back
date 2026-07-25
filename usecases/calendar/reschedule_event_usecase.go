package calendar_usecase

import (
	"strings"
	"time"

	"vozko/domain/calendar"
)

// rescheduleEventUseCase moves an existing appointment to a new time. It only computes
// the target start/end (preserving the original duration when the caller gives neither
// a new end nor a duration) and delegates the actual move — provider update, local
// persistence, and the new-slot conflict check — to the update use case, so the
// reschedule path reuses that engine rather than re-implementing it.
type rescheduleEventUseCase struct {
	repo     calendar.Repository
	google   calendar.GoogleOAuthService
	updateUC calendar.UpdateEventUseCase
}

func NewRescheduleEventUseCase(
	repo calendar.Repository,
	google calendar.GoogleOAuthService,
	updateUC calendar.UpdateEventUseCase,
) calendar.RescheduleEventUseCase {
	if repo == nil || google == nil || updateUC == nil {
		return nil
	}
	return &rescheduleEventUseCase{repo: repo, google: google, updateUC: updateUC}
}

func (uc *rescheduleEventUseCase) Execute(input calendar.RescheduleEventInput) (*calendar.CalendarEvent, error) {
	if strings.TrimSpace(input.EventID) == "" {
		return nil, calendar.ErrEventNotFound
	}
	if input.NewStartTime.IsZero() {
		return nil, calendar.ErrInvalidTimeRange
	}

	newStart := input.NewStartTime
	var newEnd time.Time
	switch {
	case input.NewEndTime != nil:
		newEnd = *input.NewEndTime
	case input.DurationMinutes > 0:
		newEnd = newStart.Add(time.Duration(input.DurationMinutes) * time.Minute)
	default:
		newEnd = newStart.Add(uc.originalDuration(input.EventID, input.WorkspaceID))
	}

	if !newEnd.After(newStart) {
		return nil, calendar.ErrInvalidTimeRange
	}

	return uc.updateUC.Execute(calendar.UpdateEventInput{
		EventID:       input.EventID,
		WorkspaceID:   input.WorkspaceID,
		UserID:        input.UserID,
		StartTime:     &newStart,
		EndTime:       &newEnd,
		SendUpdates:   input.SendUpdates,
		CheckConflict: !input.SkipConflictCheck,
	})
}

// originalDuration returns the existing event's duration so a start-only reschedule
// keeps the same length. Falls back to 30 minutes when the event cannot be resolved.
func (uc *rescheduleEventUseCase) originalDuration(eventID, workspaceID string) time.Duration {
	const fallback = 30 * time.Minute
	ev := uc.resolveExisting(eventID, workspaceID)
	if ev == nil {
		return fallback
	}
	if d := ev.EndTime.Sub(ev.StartTime); d > 0 {
		return d
	}
	return fallback
}

// resolveExisting best-effort loads the event (local cache first, Google as fallback)
// only to read its current duration; the authoritative move is done by updateUC.
func (uc *rescheduleEventUseCase) resolveExisting(eventID, workspaceID string) *calendar.CalendarEvent {
	googleEventID, isGoogleRouteID := googleCalendarEventIDFromRouteID(eventID)
	if !isGoogleRouteID {
		if ev, err := uc.repo.GetEvent(eventID, workspaceID); err == nil {
			return ev
		}
		return nil
	}
	if googleEventID == "" {
		return nil
	}
	if ev, err := uc.repo.GetEventByGoogleEventID(googleEventID, workspaceID); err == nil && ev != nil {
		return ev
	}
	conn, err := uc.repo.GetConnection(workspaceID)
	if err != nil || conn == nil {
		return nil
	}
	token, err := ensureValidToken(uc.google, uc.repo, conn)
	if err != nil {
		return nil
	}
	if ev, err := uc.google.GetGoogleEvent(token, googleEventID); err == nil {
		return ev
	}
	return nil
}
