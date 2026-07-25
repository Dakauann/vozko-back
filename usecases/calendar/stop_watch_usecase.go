package calendar_usecase

import (
	"fmt"
	"log"

	"vozko/domain/calendar"
)

type stopWatchUseCase struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewStopWatchUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) calendar.StopWatchUseCase {
	return &stopWatchUseCase{repo: repo, google: google}
}

func (uc *stopWatchUseCase) Execute(workspaceID string) error {
	ch, err := uc.repo.GetWatchChannelByWorkspace(workspaceID)
	if err != nil {
		return fmt.Errorf("get watch channel: %w", err)
	}
	if ch == nil {
		return nil
	}

	conn, err := uc.repo.GetConnection(workspaceID)
	if err != nil || conn == nil {

		log.Printf("[calendar-watch] no connection for workspace %s — deleting channel locally only", workspaceID)
		return uc.repo.DeleteWatchChannel(ch.ID)
	}

	accessToken, err := ensureValidToken(uc.google, uc.repo, conn)
	if err != nil {
		log.Printf("[calendar-watch] token refresh failed for workspace %s — deleting channel locally only: %v", workspaceID, err)
		return uc.repo.DeleteWatchChannel(ch.ID)
	}

	if err := uc.google.StopChannel(accessToken, ch.ChannelID, ch.ResourceID); err != nil {
		log.Printf("[calendar-watch] failed to stop Google channel %s: %v — removing locally", ch.ChannelID, err)
	}

	return uc.repo.DeleteWatchChannel(ch.ID)
}
