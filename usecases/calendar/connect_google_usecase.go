package calendar_usecase

import (
	"fmt"
	"log"
	"time"

	"vozko/domain/calendar"
)

type connectGoogleUseCase struct {
	repo       calendar.Repository
	google     calendar.GoogleOAuthService
	startWatch calendar.StartWatchUseCase
}

func NewConnectGoogleUseCase(repo calendar.Repository, google calendar.GoogleOAuthService, startWatch calendar.StartWatchUseCase) calendar.ConnectGoogleUseCase {
	return &connectGoogleUseCase{repo: repo, google: google, startWatch: startWatch}
}

func (uc *connectGoogleUseCase) Execute(input calendar.ConnectGoogleInput) (*calendar.GoogleCalendarConnection, error) {
	if input.Code == "" {
		return nil, fmt.Errorf("authorization code is required")
	}

	tokenResp, err := uc.google.ExchangeCode(input.Code, input.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("google token exchange: %w", err)
	}

	userInfo, err := uc.google.GetUserInfo(tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("google userinfo: %w", err)
	}

	conn := &calendar.GoogleCalendarConnection{
		WorkspaceID:  input.WorkspaceID,
		Email:        userInfo.Email,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenExpiry:  time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	if err := uc.repo.SaveConnection(conn); err != nil {
		return nil, err
	}

	if uc.startWatch != nil {
		if _, err := uc.startWatch.Execute(input.WorkspaceID); err != nil {
			log.Printf("[calendar-watch] auto-start watch failed for workspace %s: %v", input.WorkspaceID, err)

		}
	}

	return conn, nil
}
