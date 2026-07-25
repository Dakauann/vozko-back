package calendar_usecase

import (
	"vozko/domain/calendar"
)

type getAuthURLUseCase struct {
	google calendar.GoogleOAuthService
}

func NewGetAuthURLUseCase(google calendar.GoogleOAuthService) calendar.GetAuthURLUseCase {
	return &getAuthURLUseCase{google: google}
}

func (uc *getAuthURLUseCase) Execute(redirectURI, state string) string {
	return uc.google.GetAuthURL(redirectURI, state)
}
