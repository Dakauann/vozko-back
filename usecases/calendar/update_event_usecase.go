package calendar_usecase

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/calendar"
)

type updateEventUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewUpdateEventUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.UpdateEventUseCase {
	return &updateEventUseCase{repo: repo, google: google}
}

func (uc *updateEventUseCase) Execute(input calendar.UpdateEventInput) (*calendar.CalendarEvent, error) {
	googleEventID, isGoogleRouteID := googleCalendarEventIDFromRouteID(input.EventID)
	localEventID := ""
	needsGoogleFetch := false

	var existing *calendar.CalendarEvent
	var err error

	if isGoogleRouteID {
		if googleEventID == "" {
			return nil, calendar.ErrEventNotFound
		}
		existing, err = uc.repo.GetEventByGoogleEventID(googleEventID, input.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			localEventID = existing.ID
			if existing.GoogleEventID == "" {
				existing.GoogleEventID = googleEventID
			}
		} else {
			needsGoogleFetch = true
		}
	} else {
		existing, err = uc.repo.GetEvent(input.EventID, input.WorkspaceID)
		if err != nil {
			return nil, err
		}
		localEventID = existing.ID
		googleEventID = existing.GoogleEventID
	}

	var accessToken string
	if googleEventID != "" {
		conn, err := uc.repo.GetConnection(input.WorkspaceID)
		if err != nil {
			return nil, calendar.ErrGoogleNotConnected
		}
		if conn == nil {
			return nil, calendar.ErrGoogleNotConnected
		}
		accessToken, err = ensureValidToken(uc.google, uc.repo, conn)
		if err != nil {
			return nil, fmt.Errorf("google auth: %w", err)
		}

		if needsGoogleFetch {
			existing, err = uc.google.GetGoogleEvent(accessToken, googleEventID)
			if err != nil {
				return nil, fmt.Errorf("google calendar: %w", err)
			}
			if existing == nil {
				return nil, calendar.ErrEventNotFound
			}
		}
	}

	if existing == nil {
		return nil, calendar.ErrEventNotFound
	}
	if existing.ID == "" && googleEventID != "" {
		existing.ID = clientGoogleCalendarEventID(googleEventID)
	}
	if existing.WorkspaceID == "" {
		existing.WorkspaceID = input.WorkspaceID
	}
	if existing.UserID == "" {
		existing.UserID = input.UserID
	}
	if googleEventID != "" {
		existing.GoogleEventID = googleEventID
	}
	if existing.Status == "" {
		existing.Status = "confirmed"
	}

	if input.Title != nil {
		if strings.TrimSpace(*input.Title) == "" {
			return nil, calendar.ErrTitleRequired
		}
		existing.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		existing.Description = *input.Description
	}
	if input.Location != nil {
		existing.Location = *input.Location
	}
	if input.StartTime != nil {
		existing.StartTime = *input.StartTime
	}
	if input.EndTime != nil {
		existing.EndTime = *input.EndTime
	}
	if input.AllDay != nil {
		existing.AllDay = *input.AllDay
	}
	if input.TimeZone != nil {
		existing.TimeZone = *input.TimeZone
	}
	if input.Color != nil {
		existing.Color = *input.Color
	}
	if input.Attendees != nil {
		existing.Attendees = input.Attendees
	}
	if input.MeetingLink != nil {
		existing.MeetingLink = *input.MeetingLink
	}
	if input.GuestsCanModify != nil {
		existing.GuestsCanModify = *input.GuestsCanModify
	}
	if input.GuestsCanInviteOthers != nil {
		existing.GuestsCanInviteOthers = *input.GuestsCanInviteOthers
	}
	if input.GuestsCanSeeOtherGuests != nil {
		existing.GuestsCanSeeOtherGuests = *input.GuestsCanSeeOtherGuests
	}
	if input.Visibility != nil {
		existing.Visibility = *input.Visibility
	}
	if input.Transparency != nil {
		existing.Transparency = *input.Transparency
	}
	if input.Recurrence != nil {
		existing.Recurrence = input.Recurrence
	}
	if input.RemindersUseDefault != nil {
		existing.RemindersUseDefault = *input.RemindersUseDefault
	}
	if input.ReminderOverrides != nil {
		existing.ReminderOverrides = input.ReminderOverrides
	}

	if strings.TrimSpace(existing.Title) == "" {
		return nil, calendar.ErrTitleRequired
	}
	if !existing.AllDay && !existing.EndTime.After(existing.StartTime) {
		return nil, calendar.ErrInvalidTimeRange
	}

	// Reschedule guard: when asked, verify the (new) slot is free before moving the
	// event. The event being moved is excluded by its Google id, and free/transparent
	// and all-day events never block. A listing error is non-fatal (fail open).
	if input.CheckConflict && googleEventID != "" && !existing.AllDay {
		if busy, other := uc.slotOccupied(accessToken, googleEventID, existing.StartTime, existing.EndTime); busy {
			log.Printf("[calendar] reschedule conflict: slot %s–%s overlaps %q",
				existing.StartTime.Format(time.RFC3339), existing.EndTime.Format(time.RFC3339), other)
			return nil, calendar.ErrSlotConflict
		}
	}

	if googleEventID != "" {
		sendUpdates := input.SendUpdates
		if sendUpdates == "" {
			sendUpdates = "all"
		}
		if err := uc.google.UpdateGoogleEvent(accessToken, googleEventID, existing, sendUpdates); err != nil {
			return nil, fmt.Errorf("google calendar: %w", err)
		}
	}

	if localEventID != "" {
		if err := uc.repo.UpdateEvent(localEventID, existing); err != nil {
			if googleEventID == "" || !errors.Is(err, calendar.ErrEventNotFound) {
				if googleEventID == "" {
					return nil, err
				}
				log.Printf("[calendar] failed to update local cached event %s after Google update: %v", localEventID, err)
			}
		}
	}

	return existing, nil
}

// slotOccupied reports whether any busy event other than excludeGoogleID overlaps
// [start, end). Free/transparent and all-day events do not block. A listing error is
// treated as "not occupied" (fail open) so a transient Google error never blocks a move.
func (uc *updateEventUseCase) slotOccupied(accessToken, excludeGoogleID string, start, end time.Time) (bool, string) {
	events, err := uc.google.ListGoogleEvents(accessToken, start, end, "", 20)
	if err != nil {
		return false, ""
	}
	for _, ev := range events {
		if ev == nil || ev.AllDay || ev.Transparency == "transparent" {
			continue
		}
		if ev.GoogleEventID == excludeGoogleID {
			continue
		}
		if ev.StartTime.Before(end) && ev.EndTime.After(start) {
			return true, ev.Title
		}
	}
	return false, ""
}

func ensureValidToken(google calendar.GoogleOAuthService, repo calendar.Repository, conn *calendar.GoogleCalendarConnection) (string, error) {
	if time.Now().Before(conn.TokenExpiry.Add(-time.Minute)) {
		return conn.AccessToken, nil
	}
	refreshed, err := google.RefreshAccessToken(conn.RefreshToken)
	if err != nil {
		return "", err
	}
	expiry := time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
	rt := refreshed.RefreshToken
	if rt == "" {
		rt = conn.RefreshToken
	}
	if err := repo.UpdateConnectionTokens(conn.ID, refreshed.AccessToken, rt, expiry); err != nil {
		log.Printf("[calendar] failed to persist refreshed token: %v", err)
	}
	return refreshed.AccessToken, nil
}
