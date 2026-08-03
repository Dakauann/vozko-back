package calendar_usecase

import (
	"fmt"
	"log"

	"vozko/domain/calendar"
)

type handleNotificationUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewHandleNotificationUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.HandleNotificationUseCase {
	return &handleNotificationUseCase{repo: repo, google: google}
}

func (uc *handleNotificationUseCase) Execute(channelID, resourceID, token, resourceState string) error {

	if resourceState == "sync" {
		log.Printf("[calendar-watch] sync notification for channel %s, acknowledged", channelID)
		return nil
	}

	ch, err := uc.repo.GetWatchChannelByChannelID(channelID)
	if err != nil {
		return fmt.Errorf("unknown channel %s: %w", channelID, err)
	}

	if ch.Token != token {
		return fmt.Errorf("token mismatch for channel %s", channelID)
	}

	conn, err := uc.repo.GetConnection(ch.WorkspaceID)
	if err != nil || conn == nil {
		return fmt.Errorf("no google connection for workspace %s", ch.WorkspaceID)
	}

	accessToken, err := ensureValidToken(uc.google, uc.repo, conn)
	if err != nil {
		return fmt.Errorf("google auth: %w", err)
	}

	if conn.SyncToken == "" {
		log.Printf("[calendar-watch] no sync token for workspace %s, performing full sync", ch.WorkspaceID)
		return uc.fullSync(accessToken, conn)
	}

	result, err := uc.google.ListEventsIncremental(accessToken, conn.SyncToken)
	if err != nil {
		return fmt.Errorf("incremental sync: %w", err)
	}

	if result.SyncExpired {
		log.Printf("[calendar-watch] sync token expired for workspace %s, performing full sync", ch.WorkspaceID)
		return uc.fullSync(accessToken, conn)
	}

	for _, ev := range result.Events {
		uc.processEventChange(ch.WorkspaceID, ev)
	}

	if result.NextSyncToken != "" {
		if err := uc.repo.UpdateConnectionSyncToken(conn.ID, result.NextSyncToken); err != nil {
			log.Printf("[calendar-watch] failed to update sync token: %v", err)
		}
	}

	log.Printf("[calendar-watch] processed %d changes for workspace %s", len(result.Events), ch.WorkspaceID)
	return nil
}

func (uc *handleNotificationUseCase) processEventChange(workspaceID string, googleEvent *calendar.CalendarEvent) {
	if googleEvent.GoogleEventID == "" {
		return
	}

	localEvent, err := uc.repo.GetEventByGoogleEventID(googleEvent.GoogleEventID, workspaceID)
	if err != nil {
		log.Printf("[calendar-watch] error looking up event %s: %v", googleEvent.GoogleEventID, err)
		return
	}

	if localEvent == nil {

		return
	}

	if googleEvent.Status == "cancelled" {
		log.Printf("[calendar-watch] event %s (%s) was cancelled on Google Calendar", localEvent.ID, localEvent.Title)
		if err := uc.repo.UpdateEventStatus(localEvent.ID, workspaceID, "cancelled"); err != nil {
			log.Printf("[calendar-watch] failed to cancel local event %s: %v", localEvent.ID, err)
		}
		return
	}

	localEvent.Title = googleEvent.Title
	localEvent.Description = googleEvent.Description
	localEvent.Location = googleEvent.Location
	localEvent.StartTime = googleEvent.StartTime
	localEvent.EndTime = googleEvent.EndTime
	localEvent.AllDay = googleEvent.AllDay
	localEvent.TimeZone = googleEvent.TimeZone
	localEvent.Status = googleEvent.Status
	localEvent.Attendees = googleEvent.Attendees
	localEvent.MeetingLink = googleEvent.MeetingLink
	localEvent.Visibility = googleEvent.Visibility
	localEvent.Transparency = googleEvent.Transparency
	localEvent.GuestsCanModify = googleEvent.GuestsCanModify
	localEvent.GuestsCanInviteOthers = googleEvent.GuestsCanInviteOthers
	localEvent.GuestsCanSeeOtherGuests = googleEvent.GuestsCanSeeOtherGuests

	if googleEvent.Color != "" {
		localEvent.Color = googleEvent.Color
	}

	if err := uc.repo.UpdateEvent(localEvent.ID, localEvent); err != nil {
		log.Printf("[calendar-watch] failed to update local event %s: %v", localEvent.ID, err)
	}
}

func (uc *handleNotificationUseCase) fullSync(accessToken string, conn *calendar.GoogleCalendarConnection) error {

	result, err := uc.google.ListEventsIncremental(accessToken, "")
	if err != nil {

		log.Printf("[calendar-watch] full sync failed: %v, skipping", err)
		return nil
	}

	if result.NextSyncToken != "" {
		if err := uc.repo.UpdateConnectionSyncToken(conn.ID, result.NextSyncToken); err != nil {
			log.Printf("[calendar-watch] failed to save sync token after full sync: %v", err)
		}
	}
	return nil
}
