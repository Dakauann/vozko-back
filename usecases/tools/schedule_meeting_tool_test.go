package tools_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"vozko/domain/calendar"
)

func TestScheduleMeeting_Definition(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})
	def := tool.Definition()

	if def.Name != ScheduleMeetingToolName {
		t.Errorf("expected name %s, got %s", ScheduleMeetingToolName, def.Name)
	}
	if len(def.Required) != 2 {
		t.Errorf("expected 2 required params, got %d", len(def.Required))
	}
	if !def.RequiresConfig {
		t.Error("expected schedule meeting tool to require agent-level config")
	}
	for _, key := range []string{"title", "duration", "timezone", "create_google_meet", "send_updates"} {
		if _, ok := def.ConfigSchema[key]; !ok {
			t.Fatalf("expected config schema to include %s", key)
		}
	}
	if got := def.ConfigSchema["title"].DisplayName; got != "Título padrão da reunião" {
		t.Fatalf("expected professional title label, got %q", got)
	}
	if got := def.ConfigSchema["title"].Default; got != "Reunião Agendada" {
		t.Fatalf("expected title default, got %v", got)
	}
	if got := def.ConfigSchema["duration"].Default; got != 30 {
		t.Fatalf("expected 30-minute meeting duration default, got %v", got)
	}
	if got := def.ConfigSchema["create_google_meet"].Default; got != true {
		t.Fatalf("expected create_google_meet=true default, got %v", got)
	}
	sendUpdates := def.ConfigSchema["send_updates"]
	if got := sendUpdates.DisplayName; got != "Envio de convites" {
		t.Fatalf("expected professional send_updates label, got %q", got)
	}
	if got := sendUpdates.Default; got != "all" {
		t.Fatalf("expected send_updates default all, got %v", got)
	}
	if len(sendUpdates.Options) != 3 || sendUpdates.Options[1].Value != "externalOnly" || sendUpdates.Options[2].Label != "Não enviar convites" {
		t.Fatalf("expected professional send_updates options, got %+v", sendUpdates.Options)
	}
	if err := def.ValidateConfig(map[string]interface{}{
		"title":              def.ConfigSchema["title"].Default,
		"duration":           def.ConfigSchema["duration"].Default,
		"timezone":           def.ConfigSchema["timezone"].Default,
		"create_google_meet": def.ConfigSchema["create_google_meet"].Default,
		"send_updates":       def.ConfigSchema["send_updates"].Default,
	}); err != nil {
		t.Fatalf("expected schema defaults to be valid config: %v", err)
	}
	if !def.IsVisibleIn("messaging") {
		t.Error("expected visible in messaging")
	}
	if !def.IsVisibleIn("voice") {
		t.Error("expected visible in voice")
	}
}

func TestScheduleMeeting_UsesConfiguredDefaults(t *testing.T) {
	google := &mockGoogleCalendarService{createID: "event-config", meetLink: ""}
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id":     "ws-1",
		"title":              "Consulta Comercial",
		"description":        "Agendada pelo agente",
		"location":           "Online",
		"duration":           float64(45),
		"timezone":           "America/Sao_Paulo",
		"attendees":          "sales@empresa.com",
		"create_google_meet": false,
		"send_updates":       "none",
	}, map[string]interface{}{
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}
	if google.createdEvent == nil {
		t.Fatal("expected event to be created")
	}
	if google.createdEvent.Title != "Consulta Comercial" {
		t.Fatalf("expected configured title, got %q", google.createdEvent.Title)
	}
	if got := google.createdEvent.EndTime.Sub(google.createdEvent.StartTime); got != 45*time.Minute {
		t.Fatalf("expected configured 45-minute duration, got %v", got)
	}
	if google.createdMeet {
		t.Fatal("expected configured create_google_meet=false")
	}
	if google.sendUpdates != "none" {
		t.Fatalf("expected configured send_updates=none, got %q", google.sendUpdates)
	}
}

func TestScheduleMeeting_NilDeps_ReturnsNil(t *testing.T) {
	if NewScheduleMeetingToolUseCase(nil, nil) != nil {
		t.Fatal("expected nil handler when deps are nil")
	}
}

func TestScheduleMeeting_MissingWorkspaceID(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{}, map[string]interface{}{
		"title":      "Test Meeting",
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for missing workspace_id")
	}
}

func TestScheduleMeeting_NoGoogleConnection(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: nil}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Test Meeting",
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for no Google connection")
	}
}

func TestScheduleMeeting_MissingTitle(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for missing title")
	}
}

func TestScheduleMeeting_MissingStartTime(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title": "Test Meeting",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for missing start_time")
	}
}

func TestScheduleMeeting_Success_WithMeetLink(t *testing.T) {
	google := &mockGoogleCalendarService{
		createID: "google-event-123",
		meetLink: "https://meet.google.com/abc-defg-hij",
	}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Reunião de Alinhamento",
		"start_time": "2026-04-10T14:00:00-03:00",
		"duration":   float64(60),
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

	if result["google_event_id"] != "google-event-123" {
		t.Errorf("expected google_event_id=google-event-123, got %v", result["google_event_id"])
	}
	if result["meeting_link"] != "https://meet.google.com/abc-defg-hij" {
		t.Errorf("expected meeting link, got %v", result["meeting_link"])
	}
	if result["title"] != "Reunião de Alinhamento" {
		t.Errorf("expected title, got %v", result["title"])
	}
}

func TestScheduleMeeting_Success_WithEndTime(t *testing.T) {
	google := &mockGoogleCalendarService{
		createID: "event-456",
		meetLink: "",
	}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":              "Quick Sync",
		"start_time":         "2026-04-10T14:00:00-03:00",
		"end_time":           "2026-04-10T14:30:00-03:00",
		"create_google_meet": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	if result["meeting_link"] != "" {
		t.Errorf("expected no meeting link, got %v", result["meeting_link"])
	}
}

func TestScheduleMeeting_WithAttendees(t *testing.T) {
	google := &mockGoogleCalendarService{
		createID: "event-789",
		meetLink: "https://meet.google.com/xyz",
	}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Team Meeting",
		"start_time": "2026-04-10T14:00:00-03:00",
		"attendees":  "joao@empresa.com, maria@empresa.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	attendees := result["attendees"].(string)
	if attendees != "joao@empresa.com, maria@empresa.com" {
		t.Errorf("expected attendees, got %v", attendees)
	}
}

func TestScheduleMeeting_InvalidAttendeeSkippedWithNote(t *testing.T) {
	google := &mockGoogleCalendarService{createID: "event-skip", meetLink: "https://meet.google.com/abc"}
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	// A WhatsApp lead's phone number alongside a valid e-mail: the phone can't be a
	// calendar invitee, but the meeting must still be created and the AI must be
	// told what was skipped (so it shares the link in the chat).
	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Test",
		"start_time": "2026-04-10T14:00:00-03:00",
		"attendees":  "5519983401415, joao@empresa.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected meeting created despite invalid attendee, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	if result["attendees"] != "joao@empresa.com" {
		t.Errorf("expected valid attendee kept, got %v", result["attendees"])
	}
	if result["skipped_attendees"] != "5519983401415" {
		t.Errorf("expected skipped attendee reported to the AI, got %v", result["skipped_attendees"])
	}
	if _, ok := result["note"]; !ok {
		t.Error("expected a note explaining the skipped attendee so the AI can inform the user")
	}
}

func TestScheduleMeeting_EndBeforeStart(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Test",
		"start_time": "2026-04-10T14:00:00-03:00",
		"end_time":   "2026-04-10T13:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error when end is before start")
	}
}

func TestScheduleMeeting_InvalidTimezone(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Test",
		"start_time": "2026-04-10T14:00:00-03:00",
		"timezone":   "Invalid/Zone",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for invalid timezone")
	}
}

func TestScheduleMeeting_GoogleAPIError(t *testing.T) {
	google := &mockGoogleCalendarService{
		createErr: fmt.Errorf("Google API error: quota exceeded"),
	}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Test",
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result when Google API fails")
	}
}

func TestScheduleMeeting_DefaultDuration30Min(t *testing.T) {
	google := &mockGoogleCalendarService{
		createID: "event-default-dur",
	}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Quick Catch-up",
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	if result["end_time"] != "2026-04-10T14:30:00-03:00" {
		t.Errorf("expected end_time 30min after start, got %v", result["end_time"])
	}
}

func TestScheduleMeeting_ExecuteFallsBackToExecuteWithConfig(t *testing.T) {
	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, &mockGoogleCalendarService{})

	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"title":      "Test",
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error from Execute (no workspace context)")
	}
}

func TestScheduleMeeting_DeduplicatesAttendees(t *testing.T) {
	google := &mockGoogleCalendarService{createID: "event-dedup"}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Dedup Test",
		"start_time": "2026-04-10T14:00:00-03:00",
		"attendees":  "joao@empresa.com, joao@empresa.com, maria@empresa.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(res.Result.(string)), &result)

	if result["attendees"] != "joao@empresa.com, maria@empresa.com" {
		t.Errorf("expected deduplicated attendees, got %v", result["attendees"])
	}
}

func TestScheduleMeeting_WithDescriptionAndLocation(t *testing.T) {
	google := &mockGoogleCalendarService{createID: "event-full"}

	tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":       "Full Meeting",
		"start_time":  "2026-04-10T14:00:00-03:00",
		"description": "Pauta: revisão trimestral",
		"location":    "Sala 3 - Escritório",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.Result)
	}
}

func TestScheduleMeeting_TokenRefresh(t *testing.T) {
	expiredConn := validConn()
	expiredConn.TokenExpiry = expiredConn.TokenExpiry.Add(-2 * expiredConn.TokenExpiry.Sub(expiredConn.CreatedAt))

	repo := &mockCalendarRepo{conn: &calendar.GoogleCalendarConnection{
		ID:           "conn-1",
		WorkspaceID:  "ws-1",
		Email:        "cal@workspace.com",
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		TokenExpiry:  expiredConn.TokenExpiry,
	}}
	google := &mockGoogleCalendarService{
		createID:      "event-refreshed",
		refreshResult: &calendar.OAuthTokenResponse{AccessToken: "new-token", ExpiresIn: 3600},
	}

	tool := NewScheduleMeetingToolUseCase(repo, google)

	res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
		"__workspace_id": "ws-1",
	}, map[string]interface{}{
		"title":      "Refreshed Meeting",
		"start_time": "2026-04-10T14:00:00-03:00",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected success after token refresh, got error: %s", res.Result)
	}
}

func TestScheduleMeeting_CreateGoogleMeetBoolean(t *testing.T) {
	tests := []struct {
		name     string
		param    interface{}
		expectOK bool
	}{
		{"bool true", true, true},
		{"bool false", false, true},
		{"string true", "true", true},
		{"string false", "false", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			google := &mockGoogleCalendarService{createID: "event-meet-test"}
			tool := NewScheduleMeetingToolUseCase(&mockCalendarRepo{conn: validConn()}, google)

			res, err := tool.ExecuteWithConfig(context.Background(), map[string]interface{}{
				"__workspace_id": "ws-1",
			}, map[string]interface{}{
				"title":              "Meet Test",
				"start_time":         "2026-04-10T14:00:00-03:00",
				"create_google_meet": tt.param,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.IsError {
				t.Fatalf("expected success, got error: %s", res.Result)
			}
		})
	}
}
