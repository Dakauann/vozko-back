package calendar_usecase

import (
	"fmt"
	"log"

	"vozko/domain/calendar"
)

type stopWatchUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleCalendarAPI
	access calendar.AccessProvider
}

func NewStopWatchUseCase(repo calendar.Repository, google calendar.GoogleCalendarAPI, access calendar.AccessProvider) calendar.StopWatchUseCase {
	return &stopWatchUseCase{repo: repo, google: google, access: access}
}

func (uc *stopWatchUseCase) Execute(workspaceID string) error {
	ch, err := uc.repo.GetWatchChannelByWorkspace(workspaceID)
	if err != nil {
		return fmt.Errorf("get watch channel: %w", err)
	}
	if ch == nil {
		return nil
	}

	accessToken, err := calendarToken(uc.access, workspaceID)
	if err != nil {
		log.Printf("[calendar-watch] no usable google access for workspace %s, deleting channel locally only: %v", workspaceID, err)
		return uc.repo.DeleteWatchChannel(ch.ID)
	}

	if err := uc.google.StopChannel(accessToken, ch.ChannelID, ch.ResourceID); err != nil {
		log.Printf("[calendar-watch] failed to stop Google channel %s: %v, removing locally", ch.ChannelID, err)
	}

	return uc.repo.DeleteWatchChannel(ch.ID)
}
