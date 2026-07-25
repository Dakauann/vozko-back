package calendar

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAuthURLWhenGoogleOAuthDisabled(t *testing.T) {
	handler := &CalendarHandler{}
	request := httptest.NewRequest(http.MethodGet, "/calendar/google/auth-url", nil)
	recorder := httptest.NewRecorder()

	handler.GetAuthURL(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}

func TestGoogleOAuthCallbackWhenDisabled(t *testing.T) {
	handler := &CalendarHandler{}
	request := httptest.NewRequest(http.MethodGet, "/calendar/google/callback", nil)
	recorder := httptest.NewRecorder()

	handler.GoogleOAuthCallback(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "http://localhost:3000/dashboard/integrations?error=not_configured" {
		t.Fatalf("unexpected redirect location %q", location)
	}
}
