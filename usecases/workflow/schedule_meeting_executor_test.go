package workflow_usecase

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type scheduleMeetingRepoMock struct {
	connection          *calendar.GoogleCalendarConnection
	getConnectionErr    error
	lastWorkspaceID     string
	updatedTokenID      string
	updatedAccessToken  string
	updatedRefreshToken string
	updatedTokenExpiry  time.Time
	updateConnectionErr error
}

func (m *scheduleMeetingRepoMock) CreateEvent(event *calendar.CalendarEvent) error {
	return nil
}

func (m *scheduleMeetingRepoMock) UpdateEvent(eventID string, event *calendar.CalendarEvent) error {
	return nil
}

func (m *scheduleMeetingRepoMock) DeleteEvent(eventID, workspaceID string) error {
	return nil
}

func (m *scheduleMeetingRepoMock) GetEvent(eventID, workspaceID string) (*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *scheduleMeetingRepoMock) ListEvents(input calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {
	return nil, nil
}

func (m *scheduleMeetingRepoMock) SaveConnection(conn *calendar.GoogleCalendarConnection) error {
	m.connection = conn
	return nil
}

func (m *scheduleMeetingRepoMock) GetConnection(workspaceID string) (*calendar.GoogleCalendarConnection, error) {
	m.lastWorkspaceID = workspaceID
	return m.connection, m.getConnectionErr
}

func (m *scheduleMeetingRepoMock) DeleteConnection(workspaceID string) error {
	if m.connection != nil && m.connection.WorkspaceID == workspaceID {
		m.connection = nil
	}
	return nil
}

func (m *scheduleMeetingRepoMock) UpdateConnectionTokens(id, accessToken, refreshToken string, expiry time.Time) error {
	m.updatedTokenID = id
	m.updatedAccessToken = accessToken
	m.updatedRefreshToken = refreshToken
	m.updatedTokenExpiry = expiry
	return m.updateConnectionErr
}

func (m *scheduleMeetingRepoMock) UpdateConnectionSyncToken(id, syncToken string) error {
	return nil
}

func (m *scheduleMeetingRepoMock) SaveWatchChannel(ch *calendar.CalendarWatchChannel) error {
	return nil
}

func (m *scheduleMeetingRepoMock) GetWatchChannelByChannelID(channelID string) (*calendar.CalendarWatchChannel, error) {
	return nil, nil
}

func (m *scheduleMeetingRepoMock) GetWatchChannelByWorkspace(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	return nil, nil
}

func (m *scheduleMeetingRepoMock) DeleteWatchChannel(id string) error {
	return nil
}

func (m *scheduleMeetingRepoMock) ListExpiringWatchChannels(before time.Time) ([]*calendar.CalendarWatchChannel, error) {
	return nil, nil
}

func (m *scheduleMeetingRepoMock) GetEventByGoogleEventID(googleEventID, workspaceID string) (*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *scheduleMeetingRepoMock) UpdateEventStatus(eventID, workspaceID, status string) error {
	return nil
}

type scheduleMeetingGoogleMock struct {
	createErr        error
	refreshErr       error
	refreshResponse  *calendar.OAuthTokenResponse
	createdEventID   string
	createdMeetLink  string
	lastAccessToken  string
	lastCreateMeet   bool
	lastSendUpdates  string
	lastEvent        *calendar.CalendarEvent
	createCallCount  int
	refreshCallCount int
}

func (m *scheduleMeetingGoogleMock) GetAuthURL(redirectURI, state string) string {
	return ""
}

func (m *scheduleMeetingGoogleMock) ExchangeCode(code, redirectURI string) (*calendar.OAuthTokenResponse, error) {
	return nil, nil
}

func (m *scheduleMeetingGoogleMock) RefreshAccessToken(refreshToken string) (*calendar.OAuthTokenResponse, error) {
	m.refreshCallCount++
	if m.refreshErr != nil {
		return nil, m.refreshErr
	}
	return m.refreshResponse, nil
}

func (m *scheduleMeetingGoogleMock) GetUserInfo(accessToken string) (*calendar.OAuthUserInfo, error) {
	return nil, nil
}

func (m *scheduleMeetingGoogleMock) RevokeToken(token string) error {
	return nil
}

func (m *scheduleMeetingGoogleMock) CreateGoogleEvent(accessToken string, event *calendar.CalendarEvent, createMeet bool, sendUpdates string) (string, string, error) {
	m.createCallCount++
	m.lastAccessToken = accessToken
	m.lastCreateMeet = createMeet
	m.lastSendUpdates = sendUpdates
	if event != nil {
		cp := *event
		cp.Attendees = append([]calendar.Attendee(nil), event.Attendees...)
		m.lastEvent = &cp
	}
	if m.createErr != nil {
		return "", "", m.createErr
	}
	return m.createdEventID, m.createdMeetLink, nil
}

func (m *scheduleMeetingGoogleMock) UpdateGoogleEvent(accessToken string, googleEventID string, event *calendar.CalendarEvent, sendUpdates string) error {
	return nil
}

func (m *scheduleMeetingGoogleMock) GetGoogleEvent(accessToken string, googleEventID string) (*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *scheduleMeetingGoogleMock) DeleteGoogleEvent(accessToken string, googleEventID string, sendUpdates string) error {
	return nil
}

func (m *scheduleMeetingGoogleMock) ListGoogleEvents(accessToken string, timeMin, timeMax time.Time, query string, maxResults int) ([]*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *scheduleMeetingGoogleMock) WatchEvents(accessToken, channelID, token, webhookURL string) (*calendar.WatchResponse, error) {
	return nil, nil
}

func (m *scheduleMeetingGoogleMock) StopChannel(accessToken, channelID, resourceID string) error {
	return nil
}

func (m *scheduleMeetingGoogleMock) ListEventsIncremental(accessToken, syncToken string) (*calendar.IncrementalSyncResult, error) {
	return nil, nil
}

func newScheduleMeetingContext(config map[string]interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Run: &workflow.WorkflowRun{
			ID:          "run-1",
			WorkspaceID: "ws-1",
			EntryID:     "entry-1",
			EntryType:   string(shared.EntryTypeWhatsApp),
		},
		Node: &workflow.Node{
			ID:     "meeting-1",
			Type:   workflow.NodeTypeActionScheduleMeeting,
			Config: config,
		},
		Graph: &workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "meeting-1", Type: workflow.NodeTypeActionScheduleMeeting, Config: config},
				{ID: "success-node", Type: workflow.NodeTypeEnd},
				{ID: "error-node", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "meeting-1", Target: "success-node", Label: "sucesso"},
				{Source: "meeting-1", Target: "error-node", Label: "erro"},
			},
		},
		Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1"},
		State:    &state,
	}
}

func TestScheduleMeetingExecutor_Success(t *testing.T) {
	repo := &scheduleMeetingRepoMock{connection: &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(2 * time.Hour),
	}}
	google := &scheduleMeetingGoogleMock{
		createdEventID:  "g-event-1",
		createdMeetLink: "https://meet.google.com/abc-defg-hij",
	}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)
	ctx := newScheduleMeetingContext(map[string]interface{}{
		"title":              "Reunião com {{var.customer_name}}",
		"description":        "Pauta: {{var.topic}}",
		"start_time":         "{{var.start_time}}",
		"duration":           45,
		"timezone":           "America/Sao_Paulo",
		"attendees":          "alice@example.com\nBob <bob@example.com>\nalice@example.com",
		"create_google_meet": "true",
		"send_updates":       "externalOnly",
	})
	ctx.State.Set("customer_name", "Alice")
	ctx.State.Set("topic", "Q2 planning")
	ctx.State.Set("start_time", "2026-03-31T14:00:00-03:00")

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "success-node" {
		t.Fatalf("expected success edge, got %q", result.NextNodeID)
	}
	if result.Output["success"] != true {
		t.Fatalf("expected success output, got %#v", result.Output)
	}
	if google.createCallCount != 1 {
		t.Fatalf("expected CreateGoogleEvent once, got %d", google.createCallCount)
	}
	if repo.lastWorkspaceID != "ws-1" {
		t.Fatalf("expected workspace lookup ws-1, got %q", repo.lastWorkspaceID)
	}
	if google.lastAccessToken != "access-token" {
		t.Fatalf("expected original access token, got %q", google.lastAccessToken)
	}
	if google.lastSendUpdates != "externalOnly" {
		t.Fatalf("expected externalOnly updates, got %q", google.lastSendUpdates)
	}
	if !google.lastCreateMeet {
		t.Fatal("expected Google Meet to be created")
	}
	if google.lastEvent == nil {
		t.Fatal("expected captured event")
	}
	if google.lastEvent.Title != "Reunião com Alice" {
		t.Fatalf("expected interpolated title, got %q", google.lastEvent.Title)
	}
	if google.lastEvent.Description != "Pauta: Q2 planning" {
		t.Fatalf("expected interpolated description, got %q", google.lastEvent.Description)
	}
	if len(google.lastEvent.Attendees) != 2 {
		t.Fatalf("expected 2 deduplicated attendees, got %#v", google.lastEvent.Attendees)
	}
	if got := google.lastEvent.EndTime.Sub(google.lastEvent.StartTime); got != 45*time.Minute {
		t.Fatalf("expected 45 minute duration, got %s", got)
	}
	if result.Output["google_event_id"] != "g-event-1" {
		t.Fatalf("expected google_event_id, got %#v", result.Output["google_event_id"])
	}
	if result.Output["calendar_email"] != "workspace@example.com" {
		t.Fatalf("expected workspace email, got %#v", result.Output["calendar_email"])
	}
}

func TestScheduleMeetingExecutor_ReturnsErrorEdgeWhenGoogleNotConnected(t *testing.T) {
	repo := &scheduleMeetingRepoMock{}
	google := &scheduleMeetingGoogleMock{}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)
	ctx := newScheduleMeetingContext(map[string]interface{}{
		"title":      "Reunião",
		"start_time": "2026-03-31T14:00:00-03:00",
		"duration":   30,
		"timezone":   "America/Sao_Paulo",
	})

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "error-node" {
		t.Fatalf("expected error edge, got %q", result.NextNodeID)
	}
	if result.Output["success"] != false {
		t.Fatalf("expected success=false, got %#v", result.Output)
	}
	msg, _ := result.Output["error"].(string)
	if !strings.Contains(strings.ToLower(msg), "google") {
		t.Fatalf("expected Google connection error, got %q", msg)
	}
	if google.createCallCount != 0 {
		t.Fatalf("expected no Google event creation, got %d calls", google.createCallCount)
	}
}

func TestScheduleMeetingExecutor_RefreshesExpiredWorkspaceToken(t *testing.T) {
	repo := &scheduleMeetingRepoMock{connection: &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(-2 * time.Hour),
	}}
	google := &scheduleMeetingGoogleMock{
		refreshResponse: &calendar.OAuthTokenResponse{
			AccessToken:  "fresh-token",
			RefreshToken: "fresh-refresh-token",
			ExpiresIn:    3600,
		},
		createdEventID: "g-event-2",
	}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)
	ctx := newScheduleMeetingContext(map[string]interface{}{
		"title":      "Reunião",
		"start_time": "2026-03-31T14:00:00-03:00",
		"duration":   30,
		"timezone":   "America/Sao_Paulo",
	})

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["success"] != true {
		t.Fatalf("expected success after refresh, got %#v", result.Output)
	}
	if google.refreshCallCount != 1 {
		t.Fatalf("expected one refresh call, got %d", google.refreshCallCount)
	}
	if google.lastAccessToken != "fresh-token" {
		t.Fatalf("expected refreshed access token, got %q", google.lastAccessToken)
	}
	if repo.updatedTokenID != "conn-1" {
		t.Fatalf("expected token persistence for conn-1, got %q", repo.updatedTokenID)
	}
	if repo.updatedAccessToken != "fresh-token" {
		t.Fatalf("expected persisted fresh access token, got %q", repo.updatedAccessToken)
	}
	if repo.updatedRefreshToken != "fresh-refresh-token" {
		t.Fatalf("expected persisted refresh token, got %q", repo.updatedRefreshToken)
	}
}

func TestScheduleMeetingExecutor_UsesRunWorkspaceWhenWorkflowIsNil(t *testing.T) {
	repo := &scheduleMeetingRepoMock{connection: &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(time.Hour),
	}}
	google := &scheduleMeetingGoogleMock{createdEventID: "g-event-3"}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)
	ctx := newScheduleMeetingContext(map[string]interface{}{
		"title":              "Fallback workspace",
		"start_time":         "2026-03-31 14:00",
		"duration":           30,
		"timezone":           "America/Sao_Paulo",
		"create_google_meet": "false",
	})
	ctx.Workflow = nil

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["success"] != true {
		t.Fatalf("expected success, got %#v", result.Output)
	}
	if repo.lastWorkspaceID != "ws-1" {
		t.Fatalf("expected run workspace lookup, got %q", repo.lastWorkspaceID)
	}
	if google.lastCreateMeet {
		t.Fatal("expected create_google_meet=false to disable Meet creation")
	}
}

func TestScheduleMeetingExecutor_RejectsInvalidAttendees(t *testing.T) {
	repo := &scheduleMeetingRepoMock{connection: &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(time.Hour),
	}}
	google := &scheduleMeetingGoogleMock{}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)
	ctx := newScheduleMeetingContext(map[string]interface{}{
		"title":      "Reunião",
		"start_time": "2026-03-31T14:00:00-03:00",
		"duration":   30,
		"timezone":   "America/Sao_Paulo",
		"attendees":  "not-an-email",
	})

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextNodeID != "error-node" {
		t.Fatalf("expected error edge, got %q", result.NextNodeID)
	}
	msg, _ := result.Output["error"].(string)
	if !strings.Contains(strings.ToLower(msg), "convidado inválido") {
		t.Fatalf("expected invalid attendee message, got %q", msg)
	}
	if google.createCallCount != 0 {
		t.Fatalf("expected no Google event creation, got %d calls", google.createCallCount)
	}
}

// Regression: a FAILED schedule must never flow down the success edge when no
// "erro" edge is wired, it must end the run (NextNodeID empty) instead.
func TestScheduleMeetingExecutor_FailureDoesNotFallThroughToSuccess(t *testing.T) {
	repo := &scheduleMeetingRepoMock{} // no connection → failure
	google := &scheduleMeetingGoogleMock{}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)

	state := workflow.NewRunState()
	config := map[string]interface{}{
		"title":      "Reunião",
		"start_time": "2026-03-31T14:00:00-03:00",
		"duration":   30,
		"timezone":   "America/Sao_Paulo",
	}
	ctx := &workflow.NodeContext{
		Run:  &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1"},
		Node: &workflow.Node{ID: "meeting-1", Type: workflow.NodeTypeActionScheduleMeeting, Config: config},
		Graph: &workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "meeting-1", Type: workflow.NodeTypeActionScheduleMeeting, Config: config},
				{ID: "success-node", Type: workflow.NodeTypeEnd},
			},
			// ONLY the success edge is wired, no "erro".
			Edges: []workflow.Edge{
				{Source: "meeting-1", Target: "success-node", Label: "sucesso"},
			},
		},
		Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1"},
		State:    &state,
	}

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["success"] != false {
		t.Fatalf("expected failure, got %#v", result.Output)
	}
	if result.NextNodeID != "" {
		t.Fatalf("failure MUST NOT fall through to the success edge; got NextNodeID=%q", result.NextNodeID)
	}
}

// Regression: when scheduled with only start_time + title (no duration and no
// end_time, the AI's typical tool args), default to a 30-minute meeting.
func TestScheduleMeetingExecutor_DefaultsDurationWhenMissing(t *testing.T) {
	repo := &scheduleMeetingRepoMock{connection: &calendar.GoogleCalendarConnection{
		ID: "conn-1", WorkspaceID: "ws-1", Email: "w@example.com",
		AccessToken: "access-token", RefreshToken: "refresh-token",
		TokenExpiry: time.Now().Add(time.Hour),
	}}
	google := &scheduleMeetingGoogleMock{createdEventID: "g-1"}
	exec := node_executors.NewScheduleMeetingExecutor(repo, google)
	ctx := newScheduleMeetingContext(map[string]interface{}{
		"title":      "Reunião Geral",
		"start_time": "2026-06-22T13:00:00",
		"timezone":   "America/Sao_Paulo",
	})

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["success"] != true {
		t.Fatalf("expected success with default duration, got %#v", result.Output)
	}
	if google.lastEvent == nil {
		t.Fatal("expected captured event")
	}
	if got := google.lastEvent.EndTime.Sub(google.lastEvent.StartTime); got != 30*time.Minute {
		t.Fatalf("expected default 30-minute duration, got %s", got)
	}
}
