package calendar_usecase

import (
	"vozko/domain/calendar"
)

type getConnectionUseCase struct {
	repo calendar.Repository
}

func NewGetConnectionUseCase(repo calendar.Repository) calendar.GetConnectionUseCase {
	return &getConnectionUseCase{repo: repo}
}

func (uc *getConnectionUseCase) Execute(workspaceID string) (*calendar.GoogleCalendarConnection, error) {
	return uc.repo.GetConnection(workspaceID)
}
