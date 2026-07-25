package calendar_usecase

import (
	"testing"
	"time"

	"vozko/domain/calendar"
)

func validWatchConnection() *calendar.GoogleCalendarConnection {
	return &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "cal@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(time.Hour),
	}
}

func TestUpdateEvent_GoogleRouteIDFetchesFromGoogleWithoutLocalCache(t *testing.T) {
	start := time.Date(2026, 4, 27, 14, 0, 0, 0, time.UTC)
	repo := &watchRepoMock{connection: validWatchConnection()}
	google := &watchGoogleMock{
		getGoogleEventResult: &calendar.CalendarEvent{
			GoogleEventID:           "google-event-1",
			Title:                   "Old title",
			StartTime:               start,
			EndTime:                 start.Add(time.Hour),
			TimeZone:                "America/Sao_Paulo",
			Status:                  "confirmed",
			GuestsCanInviteOthers:   true,
			GuestsCanSeeOtherGuests: true,
			RemindersUseDefault:     true,
		},
	}
	useCase := NewUpdateEventUseCase(repo, google)

	title := "Updated title"
	event, err := useCase.Execute(calendar.UpdateEventInput{
		EventID:     "gcal_google-event-1",
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		Title:       &title,
		SendUpdates: "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.getEventID != "" {
		t.Fatalf("expected gcal route id to avoid local UUID lookup, got %q", repo.getEventID)
	}
	if google.gotGoogleEventID != "google-event-1" {
		t.Fatalf("expected current Google event to be fetched, got %q", google.gotGoogleEventID)
	}
	if google.updatedGoogleEventID != "google-event-1" {
		t.Fatalf("expected stripped Google event ID for update, got %q", google.updatedGoogleEventID)
	}
	if google.updatedGoogleEvent == nil || google.updatedGoogleEvent.Title != "Updated title" {
		t.Fatalf("expected updated title to be sent to Google, got %+v", google.updatedGoogleEvent)
	}
	if repo.updatedEventID != "" {
		t.Fatalf("expected no local cache update when no local row exists, got %q", repo.updatedEventID)
	}
	if event.ID != "gcal_google-event-1" || event.GoogleEventID != "google-event-1" {
		t.Fatalf("expected frontend-safe Google IDs in result, got id=%q google=%q", event.ID, event.GoogleEventID)
	}
}

func TestGetEvent_GoogleRouteIDUsesGoogleIDLookup(t *testing.T) {
	repo := &watchRepoMock{
		eventByGoogleID: &calendar.CalendarEvent{
			ID:            "local-event-id",
			WorkspaceID:   "ws-1",
			GoogleEventID: "google-event-read",
			Title:         "Read me",
		},
	}
	useCase := NewGetEventUseCase(repo)

	event, err := useCase.Execute("gcal_google-event-read", "ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.getEventID != "" {
		t.Fatalf("expected gcal route id to avoid local UUID lookup, got %q", repo.getEventID)
	}
	if event.GoogleEventID != "google-event-read" {
		t.Fatalf("expected event looked up by Google id, got %+v", event)
	}
}

func TestDeleteEvent_GoogleRouteIDDeletesGoogleWithoutLocalCache(t *testing.T) {
	repo := &watchRepoMock{connection: validWatchConnection()}
	google := &watchGoogleMock{}
	useCase := NewDeleteEventUseCase(repo, google)

	if err := useCase.Execute("gcal_google-event-2", "ws-1", "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.getEventID != "" {
		t.Fatalf("expected gcal route id to avoid local UUID lookup, got %q", repo.getEventID)
	}
	if google.deletedGoogleEventID != "google-event-2" {
		t.Fatalf("expected stripped Google event ID for delete, got %q", google.deletedGoogleEventID)
	}
	if repo.deletedEventID != "" {
		t.Fatalf("expected no local cache delete when no local row exists, got %q", repo.deletedEventID)
	}
}

func TestDeleteEvent_GoogleRouteIDAlsoDeletesLocalCacheWhenPresent(t *testing.T) {
	repo := &watchRepoMock{
		connection: validWatchConnection(),
		eventByGoogleID: &calendar.CalendarEvent{
			ID:            "local-event-id",
			WorkspaceID:   "ws-1",
			GoogleEventID: "google-event-3",
		},
	}
	google := &watchGoogleMock{}
	useCase := NewDeleteEventUseCase(repo, google)

	if err := useCase.Execute("gcal_google-event-3", "ws-1", "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if google.deletedGoogleEventID != "google-event-3" {
		t.Fatalf("expected Google event delete, got %q", google.deletedGoogleEventID)
	}
	if repo.deletedEventID != "local-event-id" {
		t.Fatalf("expected local cache cleanup, got %q", repo.deletedEventID)
	}
}
