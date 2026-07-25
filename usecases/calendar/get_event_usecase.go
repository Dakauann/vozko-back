package calendar_usecase

import (
	"vozko/domain/calendar"
)

type getEventUseCase struct {
	repo calendar.Repository
}

func NewGetEventUseCase(repo calendar.Repository) calendar.GetEventUseCase {
	return &getEventUseCase{repo: repo}
}

func (uc *getEventUseCase) Execute(eventID, workspaceID string) (*calendar.CalendarEvent, error) {
	if googleEventID, ok := googleCalendarEventIDFromRouteID(eventID); ok {
		if googleEventID == "" {
			return nil, calendar.ErrEventNotFound
		}
		event, err := uc.repo.GetEventByGoogleEventID(googleEventID, workspaceID)
		if err != nil {
			return nil, err
		}
		if event == nil {
			return nil, calendar.ErrEventNotFound
		}
		return event, nil
	}

	return uc.repo.GetEvent(eventID, workspaceID)
}
