package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/googleaccount"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type calendarEventRepoMock struct {
	connection       *calendarEventConnection
	getConnectionErr error
	lastWorkspaceID  string
}

func (m *calendarEventRepoMock) CreateEvent(event *calendar.CalendarEvent) error {
	return nil
}

func (m *calendarEventRepoMock) UpdateEvent(eventID string, event *calendar.CalendarEvent) error {
	return nil
}

func (m *calendarEventRepoMock) DeleteEvent(eventID, workspaceID string) error {
	return nil
}

func (m *calendarEventRepoMock) GetEvent(eventID, workspaceID string) (*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *calendarEventRepoMock) ListEvents(input calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {
	return nil, nil
}

func (m *calendarEventRepoMock) SaveWatchChannel(ch *calendar.CalendarWatchChannel) error {
	return nil
}

func (m *calendarEventRepoMock) GetWatchChannelByChannelID(channelID string) (*calendar.CalendarWatchChannel, error) {
	return nil, nil
}

func (m *calendarEventRepoMock) GetWatchChannelByWorkspace(workspaceID string) (*calendar.CalendarWatchChannel, error) {
	return nil, nil
}

func (m *calendarEventRepoMock) DeleteWatchChannel(id string) error {
	return nil
}

func (m *calendarEventRepoMock) ListExpiringWatchChannels(before time.Time) ([]*calendar.CalendarWatchChannel, error) {
	return nil, nil
}

func (m *calendarEventRepoMock) GetEventByGoogleEventID(googleEventID, workspaceID string) (*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *calendarEventRepoMock) UpdateEventStatus(eventID, workspaceID, status string) error {
	return nil
}

type calendarEventGoogleMock struct {
	createErr       error
	createdEventID  string
	createdMeetLink string
	lastAccessToken string
	lastCreateMeet  bool
	lastSendUpdates string
	lastEvent       *calendar.CalendarEvent
	createCallCount int
	listErr         error
}

func (m *calendarEventGoogleMock) CreateGoogleEvent(accessToken string, event *calendar.CalendarEvent, createMeet bool, sendUpdates string) (string, string, error) {
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

func (m *calendarEventGoogleMock) UpdateGoogleEvent(accessToken string, googleEventID string, event *calendar.CalendarEvent, sendUpdates string) error {
	return nil
}

func (m *calendarEventGoogleMock) GetGoogleEvent(accessToken string, googleEventID string) (*calendar.CalendarEvent, error) {
	return nil, nil
}

func (m *calendarEventGoogleMock) DeleteGoogleEvent(accessToken string, googleEventID string, sendUpdates string) error {
	return nil
}

func (m *calendarEventGoogleMock) ListGoogleEvents(accessToken string, timeMin, timeMax time.Time, query string, maxResults int) ([]*calendar.CalendarEvent, error) {
	return nil, m.listErr
}

func (m *calendarEventGoogleMock) WatchEvents(accessToken, channelID, token, webhookURL string) (*calendar.WatchResponse, error) {
	return nil, nil
}

func (m *calendarEventGoogleMock) StopChannel(accessToken, channelID, resourceID string) error {
	return nil
}

func (m *calendarEventGoogleMock) ListEventsIncremental(accessToken, syncToken string) (*calendar.IncrementalSyncResult, error) {
	return nil, nil
}

func newCreateCalendarEventContext(config map[string]interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	return &workflow.NodeContext{
		Run: &workflow.WorkflowRun{
			ID:          "run-1",
			WorkspaceID: "ws-1",
			EntryID:     "entry-1",
			EntryType:   string(shared.EntryTypeWhatsApp),
		},
		Node: &workflow.Node{
			ID:     "event-1",
			Type:   workflow.NodeTypeActionCreateCalendarEvent,
			Config: config,
		},
		Graph: &workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "event-1", Type: workflow.NodeTypeActionCreateCalendarEvent, Config: config},
				{ID: "success-node", Type: workflow.NodeTypeEnd},
				{ID: "error-node", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "event-1", Target: "success-node", Label: "sucesso"},
				{Source: "event-1", Target: "error-node", Label: "erro"},
			},
		},
		Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1"},
		State:    &state,
	}
}

func TestCreateCalendarEventExecutor_Success(t *testing.T) {
	repo := &calendarEventRepoMock{connection: &calendarEventConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(2 * time.Hour),
	}}
	google := &calendarEventGoogleMock{
		createdEventID:  "g-event-1",
		createdMeetLink: "https://meet.google.com/abc-defg-hij",
	}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)
	ctx := newCreateCalendarEventContext(map[string]interface{}{
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

func TestCreateCalendarEventExecutor_ReturnsErrorEdgeWhenGoogleNotConnected(t *testing.T) {
	repo := &calendarEventRepoMock{}
	google := &calendarEventGoogleMock{}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)
	ctx := newCreateCalendarEventContext(map[string]interface{}{
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

func TestCreateCalendarEventExecutor_UsesRunWorkspaceWhenWorkflowIsNil(t *testing.T) {
	repo := &calendarEventRepoMock{connection: &calendarEventConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(time.Hour),
	}}
	google := &calendarEventGoogleMock{createdEventID: "g-event-3"}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)
	ctx := newCreateCalendarEventContext(map[string]interface{}{
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

func TestCreateCalendarEventExecutor_RejectsInvalidAttendees(t *testing.T) {
	repo := &calendarEventRepoMock{connection: &calendarEventConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "workspace@example.com",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  time.Now().Add(time.Hour),
	}}
	google := &calendarEventGoogleMock{}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)
	ctx := newCreateCalendarEventContext(map[string]interface{}{
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

func TestCreateCalendarEventExecutor_FailureDoesNotFallThroughToSuccess(t *testing.T) {
	repo := &calendarEventRepoMock{}
	google := &calendarEventGoogleMock{}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)

	state := workflow.NewRunState()
	config := map[string]interface{}{
		"title":      "Reunião",
		"start_time": "2026-03-31T14:00:00-03:00",
		"duration":   30,
		"timezone":   "America/Sao_Paulo",
	}
	ctx := &workflow.NodeContext{
		Run:  &workflow.WorkflowRun{ID: "run-1", WorkspaceID: "ws-1"},
		Node: &workflow.Node{ID: "event-1", Type: workflow.NodeTypeActionCreateCalendarEvent, Config: config},
		Graph: &workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "event-1", Type: workflow.NodeTypeActionCreateCalendarEvent, Config: config},
				{ID: "success-node", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "event-1", Target: "success-node", Label: "sucesso"},
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

func TestCreateCalendarEventExecutor_DefaultsDurationWhenMissing(t *testing.T) {
	repo := &calendarEventRepoMock{connection: &calendarEventConnection{
		ID: "conn-1", WorkspaceID: "ws-1", Email: "w@example.com",
		AccessToken: "access-token", RefreshToken: "refresh-token",
		TokenExpiry: time.Now().Add(time.Hour),
	}}
	google := &calendarEventGoogleMock{createdEventID: "g-1"}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)
	ctx := newCreateCalendarEventContext(map[string]interface{}{
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

type calendarEventConnection struct {
	ID           string
	WorkspaceID  string
	Email        string
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
}

func (m *calendarEventRepoMock) CalendarAccess(_ context.Context, workspaceID string) (googleaccount.Access, error) {
	m.lastWorkspaceID = workspaceID
	if m.getConnectionErr != nil {
		return googleaccount.Access{}, m.getConnectionErr
	}
	if m.connection == nil {
		return googleaccount.Access{}, fmt.Errorf("%w: %w", calendar.ErrGoogleNotConnected, googleaccount.ErrNotConnected)
	}
	return googleaccount.Access{AccountID: m.connection.ID, Email: m.connection.Email, Token: m.connection.AccessToken}, nil
}

func (m *calendarEventRepoMock) GetSyncToken(string) (string, error) { return "", nil }

func (m *calendarEventRepoMock) SaveSyncToken(string, string) error { return nil }

func TestCreateCalendarEventExecutor_RefusesToBookWhenTheConflictCheckCannotRun(t *testing.T) {
	repo := &calendarEventRepoMock{connection: &calendarEventConnection{ID: "conn-1", WorkspaceID: "ws-1", AccessToken: "access-token"}}
	google := &calendarEventGoogleMock{listErr: errors.New("google unavailable")}
	exec := node_executors.NewCreateCalendarEventExecutor(repo, google, repo)
	ctx := newCreateCalendarEventContext(map[string]interface{}{"title": "Consulta", "start_time": "2026-03-31T14:00:00-03:00", "duration": 30, "timezone": "America/Sao_Paulo"})

	result, err := exec.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.NextNodeID != "error-node" || result.ErrorKind != workflow.ErrorKindHandled || result.Output["error_code"] != calendar.CodeConflictCheckUnavailable {
		t.Fatalf("booking without a conflict check: %+v", result)
	}
	if google.createCallCount != 0 {
		t.Fatalf("an event was created without checking conflicts: %d", google.createCallCount)
	}
}
