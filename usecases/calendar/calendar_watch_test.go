package calendar_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/shared"
)

type watchRepoMock struct {
	connection       *calendar.GoogleCalendarConnection
	getConnErr       error
	updatedSyncToken string

	savedChannel       *calendar.CalendarWatchChannel
	channelByID        *calendar.CalendarWatchChannel
	channelByWorkspace *calendar.CalendarWatchChannel
	expiringChannels   []*calendar.CalendarWatchChannel
	deletedChannelID   string
	saveChannelErr     error
	deleteChannelErr   error
	getChannelByIDErr  error
	getChannelByWSErr  error

	event              *calendar.CalendarEvent
	getEventErr        error
	getEventID         string
	deletedEventID     string
	deletedWorkspaceID string
	deleteEventErr     error
	eventByGoogleID    *calendar.CalendarEvent
	getEventByGIDErr   error
	updatedEventID     string
	updatedEventStatus string
	updatedEvent       *calendar.CalendarEvent
	updateEventErr     error
}

func (m *watchRepoMock) CreateEvent(event *calendar.CalendarEvent) error { return nil }
func (m *watchRepoMock) UpdateEvent(eventID string, event *calendar.CalendarEvent) error {
	m.updatedEventID = eventID
	m.updatedEvent = event
	return m.updateEventErr
}
func (m *watchRepoMock) DeleteEvent(eventID, workspaceID string) error {
	m.deletedEventID = eventID
	m.deletedWorkspaceID = workspaceID
	return m.deleteEventErr
}
func (m *watchRepoMock) GetEvent(eventID, workspaceID string) (*calendar.CalendarEvent, error) {
	m.getEventID = eventID
	return m.event, m.getEventErr
}
func (m *watchRepoMock) ListEvents(input calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {
	return nil, nil
}
func (m *watchRepoMock) SaveConnection(conn *calendar.GoogleCalendarConnection) error { return nil }
func (m *watchRepoMock) GetConnection(workspaceID string) (*calendar.GoogleCalendarConnection, error) {
	return m.connection, m.getConnErr
}
func (m *watchRepoMock) DeleteConnection(workspaceID string) error                     { return nil }
func (m *watchRepoMock) UpdateConnectionTokens(id, at, rt string, exp time.Time) error { return nil }
func (m *watchRepoMock) UpdateConnectionSyncToken(id, syncToken string) error {
	m.updatedSyncToken = syncToken
	return nil
}

func (m *watchRepoMock) SaveWatchChannel(ch *calendar.CalendarWatchChannel) error {
	m.savedChannel = ch
	return m.saveChannelErr
}
func (m *watchRepoMock) GetWatchChannelByChannelID(channelID string) (*calendar.CalendarWatchChannel, error) {
	if m.getChannelByIDErr != nil {
		return nil, m.getChannelByIDErr
	}
	return m.channelByID, nil
}
func (m *watchRepoMock) GetWatchChannelByWorkspace(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	if m.getChannelByWSErr != nil {
		return nil, m.getChannelByWSErr
	}
	return m.channelByWorkspace, nil
}
func (m *watchRepoMock) DeleteWatchChannel(id string) error {
	m.deletedChannelID = id
	return m.deleteChannelErr
}
func (m *watchRepoMock) ListExpiringWatchChannels(before time.Time) ([]*calendar.CalendarWatchChannel, error) {
	return m.expiringChannels, nil
}
func (m *watchRepoMock) GetEventByGoogleEventID(googleEventID, workspaceID string) (*calendar.CalendarEvent, error) {
	if m.getEventByGIDErr != nil {
		return nil, m.getEventByGIDErr
	}
	return m.eventByGoogleID, nil
}
func (m *watchRepoMock) UpdateEventStatus(eventID, workspaceID, status string) error {
	m.updatedEventID = eventID
	m.updatedEventStatus = status
	return nil
}

type watchGoogleMock struct {
	watchResp *calendar.WatchResponse
	watchErr  error

	stopErr       error
	stoppedChID   string
	stoppedResID  string
	stopCallCount int

	incrementalResult *calendar.IncrementalSyncResult
	incrementalErr    error
	incrementalCalls  int

	refreshResp *calendar.OAuthTokenResponse
	refreshErr  error

	getGoogleEventResult *calendar.CalendarEvent
	getGoogleEventErr    error
	gotGoogleEventID     string
	updatedGoogleEventID string
	updatedGoogleEvent   *calendar.CalendarEvent
	updateGoogleErr      error
	deletedGoogleEventID string
	deleteGoogleErr      error

	listGoogleEventsResult []*calendar.CalendarEvent
	listGoogleEventsErr    error
}

func (m *watchGoogleMock) GetAuthURL(redirectURI, state string) string { return "" }
func (m *watchGoogleMock) ExchangeCode(code, redirectURI string) (*calendar.OAuthTokenResponse, error) {
	return nil, nil
}
func (m *watchGoogleMock) RefreshAccessToken(refreshToken string) (*calendar.OAuthTokenResponse, error) {
	if m.refreshErr != nil {
		return nil, m.refreshErr
	}
	return m.refreshResp, nil
}
func (m *watchGoogleMock) GetUserInfo(accessToken string) (*calendar.OAuthUserInfo, error) {
	return nil, nil
}
func (m *watchGoogleMock) RevokeToken(token string) error { return nil }
func (m *watchGoogleMock) CreateGoogleEvent(accessToken string, event *calendar.CalendarEvent, createMeet bool, sendUpdates string) (string, string, error) {
	return "", "", nil
}
func (m *watchGoogleMock) GetGoogleEvent(accessToken string, googleEventID string) (*calendar.CalendarEvent, error) {
	m.gotGoogleEventID = googleEventID
	if m.getGoogleEventErr != nil {
		return nil, m.getGoogleEventErr
	}
	return m.getGoogleEventResult, nil
}
func (m *watchGoogleMock) UpdateGoogleEvent(accessToken string, googleEventID string, event *calendar.CalendarEvent, sendUpdates string) error {
	m.updatedGoogleEventID = googleEventID
	m.updatedGoogleEvent = event
	return m.updateGoogleErr
}
func (m *watchGoogleMock) DeleteGoogleEvent(accessToken string, googleEventID string, sendUpdates string) error {
	m.deletedGoogleEventID = googleEventID
	return m.deleteGoogleErr
}
func (m *watchGoogleMock) ListGoogleEvents(accessToken string, timeMin, timeMax time.Time, query string, maxResults int) ([]*calendar.CalendarEvent, error) {
	return m.listGoogleEventsResult, m.listGoogleEventsErr
}
func (m *watchGoogleMock) WatchEvents(accessToken, channelID, token, webhookURL string) (*calendar.WatchResponse, error) {
	if m.watchErr != nil {
		return nil, m.watchErr
	}
	return m.watchResp, nil
}
func (m *watchGoogleMock) StopChannel(accessToken, channelID, resourceID string) error {
	m.stopCallCount++
	m.stoppedChID = channelID
	m.stoppedResID = resourceID
	return m.stopErr
}
func (m *watchGoogleMock) ListEventsIncremental(accessToken, syncToken string) (*calendar.IncrementalSyncResult, error) {
	m.incrementalCalls++
	if m.incrementalErr != nil {
		return nil, m.incrementalErr
	}
	return m.incrementalResult, nil
}

type startWatchMock struct {
	result *calendar.CalendarWatchChannel
	err    error
	calls  []string
}

func (m *startWatchMock) Execute(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	m.calls = append(m.calls, workspaceID)
	return m.result, m.err
}

func validConnection() *calendar.GoogleCalendarConnection {
	return &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "user@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(2 * time.Hour),
		SyncToken:    "sync-token-1",
	}
}

func validWatchChannel() *calendar.CalendarWatchChannel {
	return &calendar.CalendarWatchChannel{
		ID:          "wch-1",
		WorkspaceID: "ws-1",
		ChannelID:   "ch-uuid-1",
		ResourceID:  "res-1",
		Token:       "secret-token",
		Expiration:  time.Now().Add(7 * 24 * time.Hour),
	}
}

func TestHandleNotification_SyncAcknowledged(t *testing.T) {
	repo := &watchRepoMock{}
	google := &watchGoogleMock{}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute("ch-1", "res-1", "tok", "sync")
	if err != nil {
		t.Fatalf("expected no error on sync notification, got: %v", err)
	}

	if google.incrementalCalls != 0 {
		t.Fatal("sync notification should not trigger incremental sync")
	}
}

func TestHandleNotification_UnknownChannel(t *testing.T) {
	repo := &watchRepoMock{getChannelByIDErr: errors.New("not found")}
	google := &watchGoogleMock{}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute("unknown-ch", "res-1", "tok", "exists")
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

func TestHandleNotification_TokenMismatch(t *testing.T) {
	ch := validWatchChannel()
	ch.Token = "correct-token"
	repo := &watchRepoMock{
		channelByID: ch,
		connection:  validConnection(),
	}
	google := &watchGoogleMock{}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute(ch.ChannelID, ch.ResourceID, "wrong-token", "exists")
	if err == nil {
		t.Fatal("expected error for token mismatch")
	}
}

func TestHandleNotification_CancelledEvent(t *testing.T) {
	ch := validWatchChannel()
	conn := validConnection()
	localEvent := &calendar.CalendarEvent{
		ID:            "local-ev-1",
		WorkspaceID:   "ws-1",
		GoogleEventID: "g-ev-1",
		Title:         "Original Title",
	}

	repo := &watchRepoMock{
		channelByID:     ch,
		connection:      conn,
		eventByGoogleID: localEvent,
	}
	google := &watchGoogleMock{
		incrementalResult: &calendar.IncrementalSyncResult{
			Events: []*calendar.CalendarEvent{
				{GoogleEventID: "g-ev-1", Status: "cancelled"},
			},
			NextSyncToken: "sync-token-2",
		},
	}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute(ch.ChannelID, ch.ResourceID, ch.Token, "exists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.updatedEventStatus != "cancelled" {
		t.Fatalf("expected status 'cancelled', got %q", repo.updatedEventStatus)
	}
	if repo.updatedEventID != "local-ev-1" {
		t.Fatalf("expected event ID 'local-ev-1', got %q", repo.updatedEventID)
	}
	if repo.updatedSyncToken != "sync-token-2" {
		t.Fatalf("expected sync token 'sync-token-2', got %q", repo.updatedSyncToken)
	}
}

func TestHandleNotification_UpdatedEvent(t *testing.T) {
	ch := validWatchChannel()
	conn := validConnection()
	localEvent := &calendar.CalendarEvent{
		ID:            "local-ev-1",
		WorkspaceID:   "ws-1",
		GoogleEventID: "g-ev-1",
		Title:         "Old Title",
	}

	repo := &watchRepoMock{
		channelByID:     ch,
		connection:      conn,
		eventByGoogleID: localEvent,
	}
	google := &watchGoogleMock{
		incrementalResult: &calendar.IncrementalSyncResult{
			Events: []*calendar.CalendarEvent{
				{
					GoogleEventID: "g-ev-1",
					Title:         "New Title",
					Description:   "Updated desc",
					Status:        "confirmed",
					Location:      "Room 42",
				},
			},
			NextSyncToken: "sync-token-3",
		},
	}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute(ch.ChannelID, ch.ResourceID, ch.Token, "exists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.updatedEventStatus != "" {
		t.Fatalf("did not expect UpdateEventStatus call, got status %q", repo.updatedEventStatus)
	}

	if repo.updatedEvent == nil {
		t.Fatal("expected UpdateEvent to be called")
	}
	if repo.updatedEvent.Title != "New Title" {
		t.Fatalf("expected title 'New Title', got %q", repo.updatedEvent.Title)
	}
	if repo.updatedEvent.Description != "Updated desc" {
		t.Fatalf("expected description 'Updated desc', got %q", repo.updatedEvent.Description)
	}
	if repo.updatedEvent.Location != "Room 42" {
		t.Fatalf("expected location 'Room 42', got %q", repo.updatedEvent.Location)
	}
}

func TestHandleNotification_SyncTokenExpired_FullResync(t *testing.T) {
	ch := validWatchChannel()
	conn := validConnection()

	repo := &watchRepoMock{
		channelByID: ch,
		connection:  conn,
	}

	callCount := 0
	google := &watchGoogleMock{
		incrementalResult: &calendar.IncrementalSyncResult{
			SyncExpired: true,
		},
	}

	origIncremental := google.incrementalResult
	google2 := &watchGoogleMock{}
	_ = origIncremental

	repo2 := &watchRepoMock{
		channelByID: ch,
		connection:  conn,
	}
	googleAdaptive := &adaptiveIncrementalMock{
		results: []*calendar.IncrementalSyncResult{
			{SyncExpired: true},
			{NextSyncToken: "fresh-sync-token", Events: nil},
		},
	}
	_ = callCount
	_ = repo
	_ = google
	_ = google2

	uc := NewHandleNotificationUseCase(repo2, googleAdaptive)
	err := uc.Execute(ch.ChannelID, ch.ResourceID, ch.Token, "exists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if googleAdaptive.callIdx != 2 {
		t.Fatalf("expected 2 incremental calls, got %d", googleAdaptive.callIdx)
	}
	if repo2.updatedSyncToken != "fresh-sync-token" {
		t.Fatalf("expected fresh-sync-token, got %q", repo2.updatedSyncToken)
	}
}

func TestHandleNotification_NoConnection(t *testing.T) {
	ch := validWatchChannel()
	repo := &watchRepoMock{
		channelByID: ch,
		connection:  nil,
	}
	google := &watchGoogleMock{}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute(ch.ChannelID, ch.ResourceID, ch.Token, "exists")
	if err == nil {
		t.Fatal("expected error when no connection")
	}
}

func TestHandleNotification_ExternalEvent_NotImported(t *testing.T) {
	ch := validWatchChannel()
	conn := validConnection()

	repo := &watchRepoMock{
		channelByID:     ch,
		connection:      conn,
		eventByGoogleID: nil,
	}
	google := &watchGoogleMock{
		incrementalResult: &calendar.IncrementalSyncResult{
			Events: []*calendar.CalendarEvent{
				{GoogleEventID: "external-ev-1", Title: "External Event", Status: "confirmed"},
			},
			NextSyncToken: "sync-token-4",
		},
	}
	uc := NewHandleNotificationUseCase(repo, google)

	err := uc.Execute(ch.ChannelID, ch.ResourceID, ch.Token, "exists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.updatedEventID != "" {
		t.Fatalf("should not update events for external events, got ID %q", repo.updatedEventID)
	}
}

func TestStartWatch_Success(t *testing.T) {
	conn := validConnection()
	conn.SyncToken = "existing-token"
	repo := &watchRepoMock{
		connection:         conn,
		channelByWorkspace: nil,
	}
	google := &watchGoogleMock{
		watchResp: &calendar.WatchResponse{
			ChannelID:  "new-ch-uuid",
			ResourceID: "new-res-id",
			Expiration: time.Now().Add(7 * 24 * time.Hour),
		},
	}
	uc := NewStartWatchUseCase(repo, google)

	ch, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected channel to be returned")
	}
	if ch.ChannelID != "new-ch-uuid" {
		t.Fatalf("expected channelID 'new-ch-uuid', got %q", ch.ChannelID)
	}
	if repo.savedChannel == nil {
		t.Fatal("expected SaveWatchChannel to be called")
	}
	if repo.savedChannel.WorkspaceID != "ws-1" {
		t.Fatalf("expected workspace 'ws-1', got %q", repo.savedChannel.WorkspaceID)
	}
	if repo.savedChannel.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestStartWatch_StopsExistingChannel(t *testing.T) {
	conn := validConnection()
	conn.SyncToken = "tok"
	existing := validWatchChannel()
	repo := &watchRepoMock{
		connection:         conn,
		channelByWorkspace: existing,
	}
	google := &watchGoogleMock{
		watchResp: &calendar.WatchResponse{
			ChannelID:  "new-ch-uuid",
			ResourceID: "new-res-id",
			Expiration: time.Now().Add(7 * 24 * time.Hour),
		},
	}
	uc := NewStartWatchUseCase(repo, google)

	_, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if google.stopCallCount != 1 {
		t.Fatalf("expected StopChannel to be called once, got %d", google.stopCallCount)
	}
	if google.stoppedChID != existing.ChannelID {
		t.Fatalf("expected StopChannel for %q, got %q", existing.ChannelID, google.stoppedChID)
	}
	if repo.deletedChannelID != existing.ID {
		t.Fatalf("expected DeleteWatchChannel for %q, got %q", existing.ID, repo.deletedChannelID)
	}
}

func TestStartWatch_NoConnection(t *testing.T) {
	repo := &watchRepoMock{connection: nil}
	google := &watchGoogleMock{}
	uc := NewStartWatchUseCase(repo, google)

	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when no Google connection")
	}
	if !errors.Is(err, calendar.ErrGoogleNotConnected) {
		t.Fatalf("expected ErrGoogleNotConnected, got: %v", err)
	}
}

func TestStartWatch_GoogleWatchFails(t *testing.T) {
	conn := validConnection()
	repo := &watchRepoMock{
		connection:         conn,
		channelByWorkspace: nil,
	}
	google := &watchGoogleMock{watchErr: errors.New("google API error")}
	uc := NewStartWatchUseCase(repo, google)

	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when Google watch fails")
	}
	if repo.savedChannel != nil {
		t.Fatal("should not save channel when watch fails")
	}
}

func TestStartWatch_SaveChannelFails_Cleanup(t *testing.T) {
	conn := validConnection()
	conn.SyncToken = "tok"
	repo := &watchRepoMock{
		connection:         conn,
		channelByWorkspace: nil,
		saveChannelErr:     errors.New("db error"),
	}
	google := &watchGoogleMock{
		watchResp: &calendar.WatchResponse{
			ChannelID:  "new-ch-uuid",
			ResourceID: "new-res-id",
			Expiration: time.Now().Add(7 * 24 * time.Hour),
		},
	}
	uc := NewStartWatchUseCase(repo, google)

	_, err := uc.Execute("ws-1")
	if err == nil {
		t.Fatal("expected error when save fails")
	}

	if google.stopCallCount != 1 {
		t.Fatalf("expected StopChannel cleanup call, got %d", google.stopCallCount)
	}
}

func TestStartWatch_SeedsSyncToken(t *testing.T) {
	conn := validConnection()
	conn.SyncToken = ""
	repo := &watchRepoMock{
		connection:         conn,
		channelByWorkspace: nil,
	}
	google := &watchGoogleMock{
		watchResp: &calendar.WatchResponse{
			ChannelID:  "new-ch-uuid",
			ResourceID: "new-res-id",
			Expiration: time.Now().Add(7 * 24 * time.Hour),
		},
		incrementalResult: &calendar.IncrementalSyncResult{
			NextSyncToken: "seeded-sync-token",
		},
	}
	uc := NewStartWatchUseCase(repo, google)

	_, err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if google.incrementalCalls != 1 {
		t.Fatalf("expected 1 incremental call for seeding, got %d", google.incrementalCalls)
	}
	if repo.updatedSyncToken != "seeded-sync-token" {
		t.Fatalf("expected seeded-sync-token, got %q", repo.updatedSyncToken)
	}
}

func TestStopWatch_Success(t *testing.T) {
	ch := validWatchChannel()
	conn := validConnection()
	repo := &watchRepoMock{
		channelByWorkspace: ch,
		connection:         conn,
	}
	google := &watchGoogleMock{}
	uc := NewStopWatchUseCase(repo, google)

	err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if google.stoppedChID != ch.ChannelID {
		t.Fatalf("expected StopChannel for %q, got %q", ch.ChannelID, google.stoppedChID)
	}
	if repo.deletedChannelID != ch.ID {
		t.Fatalf("expected DeleteWatchChannel for %q, got %q", ch.ID, repo.deletedChannelID)
	}
}

func TestStopWatch_NoExistingChannel(t *testing.T) {
	repo := &watchRepoMock{channelByWorkspace: nil}
	google := &watchGoogleMock{}
	uc := NewStopWatchUseCase(repo, google)

	err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("expected no error when no channel exists, got: %v", err)
	}
	if google.stopCallCount != 0 {
		t.Fatal("should not call StopChannel when no channel exists")
	}
}

func TestStopWatch_NoConnection_DeletesLocally(t *testing.T) {
	ch := validWatchChannel()
	repo := &watchRepoMock{
		channelByWorkspace: ch,
		connection:         nil,
	}
	google := &watchGoogleMock{}
	uc := NewStopWatchUseCase(repo, google)

	err := uc.Execute("ws-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if google.stopCallCount != 0 {
		t.Fatal("should not call Google StopChannel without connection")
	}
	if repo.deletedChannelID != ch.ID {
		t.Fatalf("expected local channel deletion, got %q", repo.deletedChannelID)
	}
}

func TestRenewChannels_RenewsExpiring(t *testing.T) {
	repo := &watchRepoMock{
		expiringChannels: []*calendar.CalendarWatchChannel{
			{ID: "ch-1", WorkspaceID: "ws-1"},
			{ID: "ch-2", WorkspaceID: "ws-2"},
		},
	}
	google := &watchGoogleMock{}
	sw := &startWatchMock{result: validWatchChannel()}
	uc := NewRenewExpiringChannelsUseCase(repo, google, sw)

	renewed, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if renewed != 2 {
		t.Fatalf("expected 2 renewed, got %d", renewed)
	}
	if len(sw.calls) != 2 {
		t.Fatalf("expected 2 StartWatch calls, got %d", len(sw.calls))
	}
	if sw.calls[0] != "ws-1" || sw.calls[1] != "ws-2" {
		t.Fatalf("wrong workspaces: %v", sw.calls)
	}
}

func TestRenewChannels_NoExpiring(t *testing.T) {
	repo := &watchRepoMock{expiringChannels: nil}
	google := &watchGoogleMock{}
	sw := &startWatchMock{}
	uc := NewRenewExpiringChannelsUseCase(repo, google, sw)

	renewed, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if renewed != 0 {
		t.Fatalf("expected 0 renewed, got %d", renewed)
	}
}

func TestRenewChannels_SkipsFailures(t *testing.T) {
	repo := &watchRepoMock{
		expiringChannels: []*calendar.CalendarWatchChannel{
			{ID: "ch-1", WorkspaceID: "ws-1"},
			{ID: "ch-2", WorkspaceID: "ws-2"},
			{ID: "ch-3", WorkspaceID: "ws-3"},
		},
	}
	google := &watchGoogleMock{}
	sw := &startWatchMock{}

	adaptiveSW := &adaptiveStartWatchMock{
		results: []startWatchResult{
			{ch: validWatchChannel(), err: nil},
			{ch: nil, err: errors.New("google error")},
			{ch: validWatchChannel(), err: nil},
		},
	}
	uc := NewRenewExpiringChannelsUseCase(repo, google, adaptiveSW)
	_ = sw

	renewed, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if renewed != 2 {
		t.Fatalf("expected 2 renewed (1 failed), got %d", renewed)
	}
	if len(adaptiveSW.calls) != 3 {
		t.Fatalf("expected 3 StartWatch calls, got %d", len(adaptiveSW.calls))
	}
}

type adaptiveIncrementalMock struct {
	watchGoogleMock
	results []*calendar.IncrementalSyncResult
	callIdx int
}

func (m *adaptiveIncrementalMock) ListEventsIncremental(accessToken, syncToken string) (*calendar.IncrementalSyncResult, error) {
	if m.callIdx >= len(m.results) {
		return nil, errors.New("unexpected extra call")
	}
	r := m.results[m.callIdx]
	m.callIdx++
	return r, nil
}

type startWatchResult struct {
	ch  *calendar.CalendarWatchChannel
	err error
}

type adaptiveStartWatchMock struct {
	results []startWatchResult
	calls   []string
	callIdx int
}

func (m *adaptiveStartWatchMock) Execute(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	m.calls = append(m.calls, workspaceID)
	if m.callIdx >= len(m.results) {
		return nil, errors.New("unexpected extra call")
	}
	r := m.results[m.callIdx]
	m.callIdx++
	return r.ch, r.err
}
