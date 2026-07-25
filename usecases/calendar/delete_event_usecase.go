package calendar_usecase

import (
	"errors"
	"fmt"
	"log"

	"vozko/domain/calendar"
)

type deleteEventUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewDeleteEventUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.DeleteEventUseCase {
	return &deleteEventUseCase{repo: repo, google: google}
}

func (uc *deleteEventUseCase) Execute(eventID, workspaceID, userID string) error {
	googleEventID, isGoogleRouteID := googleCalendarEventIDFromRouteID(eventID)
	localEventID := ""

	if isGoogleRouteID {
		if googleEventID == "" {
			return calendar.ErrEventNotFound
		}
		if event, err := uc.repo.GetEventByGoogleEventID(googleEventID, workspaceID); err != nil {
			return err
		} else if event != nil {
			localEventID = event.ID
		}
	} else {
		event, err := uc.repo.GetEvent(eventID, workspaceID)
		if err != nil {
			return err
		}
		localEventID = event.ID
		googleEventID = event.GoogleEventID
	}

	if googleEventID != "" {
		conn, err := uc.repo.GetConnection(workspaceID)
		if err != nil {
			return calendar.ErrGoogleNotConnected
		}
		if conn == nil {
			return calendar.ErrGoogleNotConnected
		}
		accessToken, err := ensureValidToken(uc.google, uc.repo, conn)
		if err != nil {
			return fmt.Errorf("google auth: %w", err)
		}
		if err := uc.google.DeleteGoogleEvent(accessToken, googleEventID, "all"); err != nil {
			return fmt.Errorf("google calendar: %w", err)
		}
	}

	if localEventID == "" {
		if googleEventID == "" {
			return calendar.ErrEventNotFound
		}
		return nil
	}

	if err := uc.repo.DeleteEvent(localEventID, workspaceID); err != nil && !errors.Is(err, calendar.ErrEventNotFound) {
		if googleEventID == "" {
			return err
		}
		log.Printf("[calendar] failed to delete local cached event %s after Google delete: %v", localEventID, err)
	}

	return nil
}
