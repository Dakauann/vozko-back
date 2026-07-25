package calendar_usecase

import (
	"log"
	"time"

	"vozko/domain/calendar"
)

type renewExpiringChannelsUseCase struct {
	repo       calendar.Repository
	google     calendar.GoogleOAuthService
	startWatch calendar.StartWatchUseCase
}

func NewRenewExpiringChannelsUseCase(repo calendar.Repository, google calendar.GoogleOAuthService, startWatch calendar.StartWatchUseCase) calendar.RenewExpiringChannelsUseCase {
	return &renewExpiringChannelsUseCase{repo: repo, google: google, startWatch: startWatch}
}

func (uc *renewExpiringChannelsUseCase) Execute() (int, error) {

	threshold := time.Now().Add(1 * time.Hour)
	channels, err := uc.repo.ListExpiringWatchChannels(threshold)
	if err != nil {
		return 0, err
	}

	renewed := 0
	for _, ch := range channels {
		if _, err := uc.startWatch.Execute(ch.WorkspaceID); err != nil {
			log.Printf("[calendar-watch] failed to renew channel for workspace %s: %v", ch.WorkspaceID, err)
			continue
		}
		renewed++
		log.Printf("[calendar-watch] renewed channel for workspace %s", ch.WorkspaceID)
	}

	return renewed, nil
}
