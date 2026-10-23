package tools_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/googleaccount"
	"vozko/domain/shared"
	"vozko/domain/tools"
)

type testCalendarConnection struct {
	Email       string
	AccessToken string
}

type mockCalendarRepo struct {
	conn *testCalendarConnection
}

func (m *mockCalendarRepo) CalendarAccess(context.Context, string) (googleaccount.Access, error) {
	if m.conn == nil {
		return googleaccount.Access{}, fmt.Errorf("%w: %w", calendar.ErrGoogleNotConnected, googleaccount.ErrNotConnected)
	}
	return googleaccount.Access{Email: m.conn.Email, Token: m.conn.AccessToken}, nil
}

func (m *mockCalendarRepo) GetSyncToken(string) (string, error) { return "", nil }
func (m *mockCalendarRepo) SaveSyncToken(string, string) error  { return nil }

func (m *mockCalendarRepo) CreateEvent(*calendar.CalendarEvent) error                { return nil }
func (m *mockCalendarRepo) UpdateEvent(string, *calendar.CalendarEvent) error        { return nil }
func (m *mockCalendarRepo) DeleteEvent(string, string) error                         { return nil }
func (m *mockCalendarRepo) GetEvent(string, string) (*calendar.CalendarEvent, error) { return nil, nil }
func (m *mockCalendarRepo) ListEvents(calendar.ListEventsInput) (*shared.PaginatedResult[*calendar.CalendarEvent], error) {
	return nil, nil
}
func (m *mockCalendarRepo) SaveWatchChannel(*calendar.CalendarWatchChannel) error   { return nil }
func (m *mockCalendarRepo) GetWatchChannelByChannelID(string) (*calendar.CalendarWatchChannel, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetWatchChannelByWorkspace(string) (*calendar.CalendarWatchChannel, error) {
	return nil, nil
}
func (m *mockCalendarRepo) DeleteWatchChannel(string) error { return nil }
func (m *mockCalendarRepo) ListExpiringWatchChannels(time.Time) ([]*calendar.CalendarWatchChannel, error) {
	return nil, nil
}
func (m *mockCalendarRepo) GetEventByGoogleEventID(string, string) (*calendar.CalendarEvent, error) {
	return nil, nil
}
func (m *mockCalendarRepo) UpdateEventStatus(string, string, string) error { return nil }

type mockGoogleCalendarService struct {
	events        []*calendar.CalendarEvent
	listErr       error
	createID      string
	meetLink      string
	createErr     error
	createdEvent  *calendar.CalendarEvent
	createdMeet   bool
	sendUpdates   string
}

func (m *mockGoogleCalendarService) CreateGoogleEvent(accessToken string, event *calendar.CalendarEvent, createMeet bool, sendUpdates string) (string, string, error) {
	m.createdEvent = event
	m.createdMeet = createMeet
	m.sendUpdates = sendUpdates
	return m.createID, m.meetLink, m.createErr
}
func (m *mockGoogleCalendarService) UpdateGoogleEvent(string, string, *calendar.CalendarEvent, string) error {
	return nil
}
func (m *mockGoogleCalendarService) GetGoogleEvent(string, string) (*calendar.CalendarEvent, error) {
	return nil, nil
}
func (m *mockGoogleCalendarService) DeleteGoogleEvent(string, string, string) error { return nil }
func (m *mockGoogleCalendarService) ListGoogleEvents(accessToken string, timeMin, timeMax time.Time, query string, maxResults int) ([]*calendar.CalendarEvent, error) {
	return m.events, m.listErr
}
func (m *mockGoogleCalendarService) WatchEvents(string, string, string, string) (*calendar.WatchResponse, error) {
	return nil, nil
}
func (m *mockGoogleCalendarService) StopChannel(string, string, string) error { return nil }
func (m *mockGoogleCalendarService) ListEventsIncremental(string, string) (*calendar.IncrementalSyncResult, error) {
	return nil, nil
}

func validConn() *testCalendarConnection {
	return &testCalendarConnection{Email: "cal@workspace.com", AccessToken: "valid-token"}
}

func newAvailabilityTool(repo *mockCalendarRepo, google *mockGoogleCalendarService) tools.Handler {
	return NewCheckCalendarAvailabilityToolUseCase(repo, google, repo)
}

func newScheduleMeetingTool(repo *mockCalendarRepo, google *mockGoogleCalendarService) tools.Handler {
	return NewScheduleMeetingToolUseCase(repo, google, repo)
}

func TestCheckCalendarAvailability_Definition(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})
	def := tool.Definition()

	if def.Name != CheckCalendarAvailabilityToolName {
		t.Errorf("expected name %s, got %s", CheckCalendarAvailabilityToolName, def.Name)
	}
	if len(def.Required) != 1 || def.Required[0] != "date_from" {
		t.Errorf("expected required=[date_from], got %v", def.Required)
	}
	if !def.RequiresConfig {
		t.Error("expected calendar availability tool to require agent-level config")
	}
	for _, key := range []string{"timezone", "slot_duration", "working_start", "working_end", "work_days"} {
		if _, ok := def.ConfigSchema[key]; !ok {
			t.Fatalf("expected config schema to include %s", key)
		}
	}
	if got := def.ConfigSchema["timezone"].DisplayName; got != "Fuso horário padrão" {
		t.Fatalf("expected professional timezone label, got %q", got)
	}
	if got := def.ConfigSchema["timezone"].Default; got != "America/Sao_Paulo" {
		t.Fatalf("expected timezone default America/Sao_Paulo, got %v", got)
	}
	if got := def.ConfigSchema["slot_duration"].Default; got != 30 {
		t.Fatalf("expected 30-minute slot default, got %v", got)
	}
	workDays := def.ConfigSchema["work_days"]
	if got := workDays.DisplayName; got != "Dias disponíveis" {
		t.Fatalf("expected professional work_days label, got %q", got)
	}
	if got := workDays.Default; got != "weekdays" {
		t.Fatalf("expected work_days default weekdays, got %v", got)
	}
	if len(workDays.Options) != 3 || workDays.Options[0].Value != "weekdays" || workDays.Options[0].Label != "Segunda a sexta" {
		t.Fatalf("expected professional work_days options, got %+v", workDays.Options)
	}
	if err := def.ValidateConfig(map[string]interface{}{
		"timezone":      def.ConfigSchema["timezone"].Default,
		"slot_duration": def.ConfigSchema["slot_duration"].Default,
		"working_start": def.ConfigSchema["working_start"].Default,
		"working_end":   def.ConfigSchema["working_end"].Default,
		"work_days":     def.ConfigSchema["work_days"].Default,
	}); err != nil {
		t.Fatalf("expected schema defaults to be valid config: %v", err)
	}
	if !def.IsVisibleIn("messaging") {
		t.Error("expected visible in messaging")
	}
}

func TestCheckCalendarAvailability_UsesConfiguredDefaults(t *testing.T) {
	google := &mockGoogleCalendarService{}
	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		google,
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
		"timezone":       "America/Sao_Paulo",
		"slot_duration":  float64(45),
		"working_start":  "09:00",
		"working_end":    "10:30",
		"work_days":      "weekdays",
	}, map[string]interface{}{
		"date_from": "2026-04-10T09:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(res.Result.(string)), &result); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if result["date_to"] != "2026-04-10T10:30:00-03:00" {
		t.Fatalf("expected configured working_end to define date_to, got %v", result["date_to"])
	}
	if len(result["available_slots_json"].([]interface{})) != 2 {
		t.Fatalf("expected two configured 45-minute slots, got %v", result["available_slots_json"])
	}
}

func TestCheckCalendarAvailability_NilDeps_ReturnsNil(t *testing.T) {
	if NewCheckCalendarAvailabilityToolUseCase(nil, nil, nil) != nil {
		t.Fatal("expected nil handler when deps are nil")
	}
}

func TestCheckCalendarAvailability_MissingWorkspaceID(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{}, map[string]interface{}{
		"date_from": "2026-04-10T08:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result for missing workspace_id")
	}
}

func TestCheckCalendarAvailability_NoGoogleConnection(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: nil}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from": "2026-04-10T08:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for no Google connection")
	}
}

func TestCheckCalendarAvailability_MissingDateFrom(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for missing date_from")
	}
}

func TestCheckCalendarAvailability_InvalidTimezone(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from": "2026-04-10T08:00:00-03:00",
		"timezone":  "Invalid/Zone",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for invalid timezone")
	}
}

func TestCheckCalendarAvailability_EmptyCalendar_AllSlotsFree(t *testing.T) {
	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from":     "2026-04-10T08:00:00-03:00",
		"date_to":       "2026-04-10T12:00:00-03:00",
		"timezone":      "America/Sao_Paulo",
		"slot_duration": float64(30),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(res.Result.(string)), &result); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	slots, ok := result["available_slots_json"].([]interface{})
	if !ok {
		t.Fatalf("expected available_slots_json array")
	}

	if len(slots) != 8 {
		t.Errorf("expected 8 slots, got %d", len(slots))
	}
}

func TestCheckCalendarAvailability_BusyEvents_SlotsReduced(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	busyStart := time.Date(2026, 4, 10, 9, 0, 0, 0, loc)
	busyEnd := time.Date(2026, 4, 10, 10, 0, 0, 0, loc)

	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{
			{StartTime: busyStart, EndTime: busyEnd, Transparency: "opaque"},
		}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from":     "2026-04-10T08:00:00-03:00",
		"date_to":       "2026-04-10T12:00:00-03:00",
		"timezone":      "America/Sao_Paulo",
		"slot_duration": float64(30),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	slots := result["available_slots_json"].([]interface{})

	if len(slots) != 6 {
		t.Errorf("expected 6 slots (3h free / 30min), got %d", len(slots))
	}

	busyCount := result["busy_count"].(float64)
	if busyCount != 1 {
		t.Errorf("expected busy_count=1, got %v", busyCount)
	}
}

func TestCheckCalendarAvailability_TransparentEvents_Ignored(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	transparentStart := time.Date(2026, 4, 10, 9, 0, 0, 0, loc)
	transparentEnd := time.Date(2026, 4, 10, 10, 0, 0, 0, loc)

	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{
			{StartTime: transparentStart, EndTime: transparentEnd, Transparency: "transparent"},
		}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from":     "2026-04-10T08:00:00-03:00",
		"date_to":       "2026-04-10T12:00:00-03:00",
		"timezone":      "America/Sao_Paulo",
		"slot_duration": float64(30),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	slots := result["available_slots_json"].([]interface{})

	if len(slots) != 8 {
		t.Errorf("expected 8 slots (transparent ignored), got %d", len(slots))
	}
}

func TestCheckCalendarAvailability_DefaultDateTo_EndOfBusiness(t *testing.T) {
	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from":     "2026-04-10T14:00:00-03:00",
		"timezone":      "America/Sao_Paulo",
		"slot_duration": float64(60),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	slots := result["available_slots_json"].([]interface{})

	if len(slots) != 4 {
		t.Errorf("expected 4 slots (14-18h with 60min), got %d", len(slots))
	}
}


func TestCheckCalendarAvailability_DateToBeforeFrom(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from": "2026-04-10T18:00:00-03:00",
		"date_to":   "2026-04-10T08:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error when date_to is before date_from")
	}
}

func TestCheckCalendarAvailability_InvalidDateFromFormat(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from": "not-a-date",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for invalid date format")
	}
}

func TestCheckCalendarAvailability_ExecuteFallsBackToExecuteWithConfig(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"date_from": "2026-04-10T08:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error from Execute (no workspace context)")
	}
}

func TestCheckCalendarAvailability_DateOnlyFormat(t *testing.T) {
	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from":     "2026-04-10",
		"date_to":       "2026-04-10T18:00:00-03:00",
		"timezone":      "America/Sao_Paulo",
		"slot_duration": float64(60),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success with date-only format, got error: %s", res.Result)
	}
}

func TestCheckCalendarAvailability_MultipleOverlappingBusy(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")

	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{
			{StartTime: time.Date(2026, 4, 10, 9, 0, 0, 0, loc), EndTime: time.Date(2026, 4, 10, 10, 0, 0, 0, loc), Transparency: "opaque"},
			{StartTime: time.Date(2026, 4, 10, 9, 30, 0, 0, loc), EndTime: time.Date(2026, 4, 10, 10, 30, 0, 0, loc), Transparency: "opaque"},
		}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from":     "2026-04-10T08:00:00-03:00",
		"date_to":       "2026-04-10T12:00:00-03:00",
		"timezone":      "America/Sao_Paulo",
		"slot_duration": float64(30),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	slots := result["available_slots_json"].([]interface{})

	if len(slots) != 5 {
		t.Errorf("expected 5 slots (overlapping events merged), got %d", len(slots))
	}
}

func TestCheckCalendarAvailability_ResultContainsCalendarEmail(t *testing.T) {
	tool := newAvailabilityTool(
		&mockCalendarRepo{conn: validConn()},
		&mockGoogleCalendarService{events: []*calendar.CalendarEvent{}},
	)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"date_from": "2026-04-10T08:00:00-03:00",
		"date_to":   "2026-04-10T09:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	if result["calendar_email"] != "cal@workspace.com" {
		t.Errorf("expected calendar_email=cal@workspace.com, got %v", result["calendar_email"])
	}
}

func TestCheckCalendarAvailability_AcceptsAWholeDayLikeTheWorkflowStep(t *testing.T) {
	tool := newAvailabilityTool(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{events: []*calendar.CalendarEvent{}})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{"__workspace_id": "ws-1"}, map[string]interface{}{
		"date_from":     "2026-04-10",
		"date_to":       "2026-04-10",
		"timezone":      "America/Sao_Paulo",
		"working_start": "08:00",
		"working_end":   "12:00",
		"slot_duration": float64(60),
	})
	if err != nil || res.IsError {
		t.Fatalf("whole day refused: %v %v", err, res.Result)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(res.Result.(string)), &result); err != nil {
		t.Fatal(err)
	}
	if result["date_from"] != "2026-04-10T08:00:00-03:00" || result["date_to"] != "2026-04-10T12:00:00-03:00" {
		t.Fatalf("range = %v to %v", result["date_from"], result["date_to"])
	}
	if text, _ := result["available_slots"].(string); !strings.HasPrefix(text, "10/04 08:00 a 09:00") {
		t.Fatalf("slot text = %q", text)
	}
	if slots, _ := result["available_slots_json"].([]interface{}); len(slots) != 4 {
		t.Fatalf("slots = %v", result["available_slots_json"])
	}
}
