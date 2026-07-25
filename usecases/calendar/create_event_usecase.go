package calendar_usecase

import (
	"fmt"

	"vozko/domain/calendar"
)

type createEventUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewCreateEventUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.CreateEventUseCase {
	return &createEventUseCase{repo: repo, google: google}
}

func (uc *createEventUseCase) Execute(input calendar.CreateEventInput) (*calendar.CalendarEvent, error) {
	if input.Title == "" {
		return nil, calendar.ErrTitleRequired
	}
	if !input.AllDay && !input.EndTime.After(input.StartTime) {
		return nil, calendar.ErrInvalidTimeRange
	}

	conn, err := uc.repo.GetConnection(input.WorkspaceID)
	if err != nil {
		return nil, calendar.ErrGoogleNotConnected
	}
	if conn == nil {
		return nil, calendar.ErrGoogleNotConnected
	}

	accessToken, err := ensureValidToken(uc.google, uc.repo, conn)
	if err != nil {
		return nil, fmt.Errorf("google auth: %w", err)
	}

	event := &calendar.CalendarEvent{
		WorkspaceID:             input.WorkspaceID,
		UserID:                  input.UserID,
		Title:                   input.Title,
		Description:             input.Description,
		Location:                input.Location,
		StartTime:               input.StartTime,
		EndTime:                 input.EndTime,
		AllDay:                  input.AllDay,
		TimeZone:                input.TimeZone,
		Color:                   input.Color,
		Attendees:               input.Attendees,
		MeetingLink:             input.MeetingLink,
		Status:                  "confirmed",
		GuestsCanModify:         input.GuestsCanModify,
		GuestsCanInviteOthers:   boolOrDefault(input.GuestsCanInviteOthers, true),
		GuestsCanSeeOtherGuests: boolOrDefault(input.GuestsCanSeeOtherGuests, true),
		Visibility:              input.Visibility,
		Transparency:            input.Transparency,
		Recurrence:              input.Recurrence,
		RemindersUseDefault:     input.RemindersUseDefault,
		ReminderOverrides:       input.ReminderOverrides,
	}

	sendUpdates := input.SendUpdates
	if sendUpdates == "" {
		sendUpdates = "all"
	}
	googleID, meetLink, err := uc.google.CreateGoogleEvent(accessToken, event, input.CreateGoogleMeet, sendUpdates)
	if err != nil {
		return nil, fmt.Errorf("google calendar: %w", err)
	}
	event.GoogleEventID = googleID
	if meetLink != "" {
		event.MeetingLink = meetLink
	}

	if err := uc.repo.CreateEvent(event); err != nil {

		_ = uc.google.DeleteGoogleEvent(accessToken, googleID, "")
		return nil, err
	}
	return event, nil
}

func boolOrDefault(ptr *bool, def bool) bool {
	if ptr == nil {
		return def
	}
	return *ptr
}
