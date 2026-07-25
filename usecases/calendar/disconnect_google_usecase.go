package calendar_usecase

import (
	"log"

	"vozko/domain/calendar"
)

type disconnectGoogleUseCase struct {
	repo      calendar.Repository
	google    calendar.GoogleOAuthService
	stopWatch calendar.StopWatchUseCase
}

func NewDisconnectGoogleUseCase(repo calendar.Repository, google calendar.GoogleOAuthService, stopWatch calendar.StopWatchUseCase) calendar.DisconnectGoogleUseCase {
	return &disconnectGoogleUseCase{repo: repo, google: google, stopWatch: stopWatch}
}

func (uc *disconnectGoogleUseCase) Execute(workspaceID string) error {

	if uc.stopWatch != nil {
		if err := uc.stopWatch.Execute(workspaceID); err != nil {
			log.Printf("[calendar] failed to stop watch for ws=%s: %v", workspaceID, err)
		}
	}

	conn, err := uc.repo.GetConnection(workspaceID)
	if err == nil && conn != nil && conn.RefreshToken != "" {
		if err := uc.google.RevokeToken(conn.RefreshToken); err != nil {
			log.Printf("[calendar] failed to revoke google token for ws=%s: %v", workspaceID, err)
		}
	}

	return uc.repo.DeleteConnection(workspaceID)
}
