package scheduledmessage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	conversationdomain "vozko/domain/conversation"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

// The delivery layer's job is translation: a frame in, a status and a code out.
// These pin the two things a client cannot recover from if we get them wrong —
// which status each refusal carries, and whether the refusal names the boundary
// the operator has to correct against.

var reqNow = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

type stubSchedule struct {
	result *sm.ScheduleResult
	err    error
	last   sm.ScheduleInput
}

func (s *stubSchedule) Execute(_ context.Context, in sm.ScheduleInput) (*sm.ScheduleResult, error) {
	s.last = in
	return s.result, s.err
}

type stubReschedule struct {
	result *sm.ScheduleResult
	err    error
}

func (s *stubReschedule) Execute(context.Context, sm.RescheduleInput) (*sm.ScheduleResult, error) {
	return s.result, s.err
}

type stubCancel struct{ err error }

func (s *stubCancel) Execute(context.Context, string, string) error { return s.err }

type stubList struct {
	entryResult *sm.ListForEntryResult
	err         error
}

func (s *stubList) ForEntry(context.Context, string, string, []sm.Status) (*sm.ListForEntryResult, error) {
	return s.entryResult, s.err
}
func (s *stubList) ForWorkspace(context.Context, string, sm.ListQuery) ([]*sm.ScheduledMessage, int64, error) {
	return nil, 0, s.err
}

type stubAuthorizer struct{ allow bool }

func (a stubAuthorizer) CanAccessEntry(string, string, string, string, bool) bool { return a.allow }
func (a stubAuthorizer) CanAccessCampaign(string, string, string, string, bool) bool {
	return a.allow
}
func (a stubAuthorizer) GetAccessibleEntryIDs(string, string, bool) []string { return nil }
func (a stubAuthorizer) GetDepartmentScope(string, string, bool) (conversationdomain.DepartmentAccessScope, bool) {
	return conversationdomain.DepartmentAccessScope{}, false
}
func (a stubAuthorizer) HasWorkspacePermission(string, string, string, string, bool) bool {
	return true
}
func (a stubAuthorizer) IsWorkspaceMember(string, string) bool       { return true }
func (a stubAuthorizer) IsWorkspaceOwnerOrAdmin(string, string) bool { return true }

type handlerFixture struct {
	schedule   *stubSchedule
	reschedule *stubReschedule
	cancel     *stubCancel
	list       *stubList
	router     *mux.Router
}

func newHandlerFixture(allowAccess bool) *handlerFixture {
	f := &handlerFixture{
		schedule:   &stubSchedule{},
		reschedule: &stubReschedule{},
		cancel:     &stubCancel{},
		list:       &stubList{},
	}
	h := NewScheduledMessageHandler(f.schedule, f.reschedule, f.cancel, f.list, stubAuthorizer{allow: allowAccess})

	f.router = mux.NewRouter()
	f.router.HandleFunc("/conversations/{entryType}/{entryId}/scheduled-messages", h.Create).Methods(http.MethodPost)
	f.router.HandleFunc("/conversations/{entryType}/{entryId}/scheduled-messages", h.List).Methods(http.MethodGet)
	f.router.HandleFunc("/scheduled-messages/{id}", h.Reschedule).Methods(http.MethodPatch)
	f.router.HandleFunc("/scheduled-messages/{id}", h.Cancel).Methods(http.MethodDelete)
	return f
}

func (f *handlerFixture) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// GetWorkspaceID falls back to this header, so no workspace middleware is
	// needed to exercise the handler.
	req.Header.Set("X-Workspace-ID", "ws-1")
	req = req.WithContext(context.WithValue(req.Context(),
		middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"}))

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func message() *sm.ScheduledMessage {
	return &sm.ScheduledMessage{
		ID:          "sched-1",
		WorkspaceID: "ws-1",
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeWhatsApp,
		Text:        "oi",
		ScheduledAt: reqNow.Add(2 * time.Hour),
		Status:      sm.StatusPending,
	}
}

func openWindow() sm.WindowState {
	expires := reqNow.Add(6 * time.Hour)
	return sm.WindowState{Open: true, ExpiresAt: &expires, LatestAllowedAt: &expires}
}

func TestCreateReturns201(t *testing.T) {
	f := newHandlerFixture(true)
	f.schedule.result = &sm.ScheduleResult{Message: message(), Window: openWindow()}

	rec := f.do(http.MethodPost, "/conversations/whatsapp/entry-1/scheduled-messages",
		`{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z"}`, nil)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

// A replayed idempotency key created nothing. Answering 201 would tell a
// retrying client it had just made a second message.
func TestCreateReturns200ForAReplayedKey(t *testing.T) {
	f := newHandlerFixture(true)
	f.schedule.result = &sm.ScheduleResult{Message: message(), Window: openWindow(), AlreadyExisted: true}

	rec := f.do(http.MethodPost, "/conversations/whatsapp/entry-1/scheduled-messages",
		`{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z"}`,
		map[string]string{idempotencyHeader: "key-1"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a replay", rec.Code)
	}
	if f.schedule.last.IdempotencyKey != "key-1" {
		t.Errorf("the idempotency header did not reach the use case: %q", f.schedule.last.IdempotencyKey)
	}
}

func TestCreateForwardsTheWholeComposedMessage(t *testing.T) {
	f := newHandlerFixture(true)
	f.schedule.result = &sm.ScheduleResult{Message: message(), Window: openWindow()}

	f.do(http.MethodPost, "/conversations/instagram/entry-9/scheduled-messages",
		`{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z","media_id":"med-1","media_type":"image","reply_to_message_id":"msg-9","signed":true}`, nil)

	in := f.schedule.last
	if in.EntryID != "entry-9" || in.EntryType != "instagram" {
		t.Errorf("entry = %s/%s", in.EntryType, in.EntryID)
	}
	if in.MediaID != "med-1" || in.MediaType != "image" || in.ReplyToMessageID != "msg-9" || !in.Signed {
		t.Errorf("input = %+v", in)
	}
	if in.WorkspaceID != "ws-1" {
		t.Errorf("workspace = %q, want the scoped workspace", in.WorkspaceID)
	}
}

// Every refusal that is ABOUT the window carries the window, because a refusal
// that does not name the boundary makes the next attempt a guess.
func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantWindow bool
	}{
		{"closed window", sm.ErrWindowClosed, http.StatusConflict, "window_closed", true},
		{"past the window", sm.ErrScheduledAtPastWindow, http.StatusUnprocessableEntity, "past_window", true},
		{"too soon", sm.ErrScheduledAtTooSoon, http.StatusUnprocessableEntity, "too_soon", true},
		{"too far", sm.ErrScheduledAtTooFar, http.StatusUnprocessableEntity, "too_far", true},
		{"not found", sm.ErrNotFound, http.StatusNotFound, "not_found", false},
		{"already sent", sm.ErrNotPending, http.StatusConflict, "not_pending", false},
		{"no content", sm.ErrContentRequired, http.StatusBadRequest, "invalid_request", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newHandlerFixture(true)
			f.schedule.err = tc.err
			f.schedule.result = &sm.ScheduleResult{Window: openWindow()}

			rec := f.do(http.MethodPost, "/conversations/whatsapp/entry-1/scheduled-messages",
				`{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z"}`, nil)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unparseable body: %v", err)
			}
			if body["code"] != tc.wantCode {
				t.Errorf("code = %v, want %q", body["code"], tc.wantCode)
			}
			if tc.wantWindow {
				window, ok := body["window"].(map[string]any)
				if !ok || window["latestAllowedAt"] == nil {
					t.Errorf("the refusal did not name the boundary: %s", rec.Body.String())
				}
			}
		})
	}
}

// The route's permission gate answers "may this ROLE schedule"; this answers
// "may this USER see this conversation". Department scoping makes them
// different questions.
func TestEntryRoutesRefuseAConversationTheUserCannotSee(t *testing.T) {
	f := newHandlerFixture(false)

	for _, tc := range []struct{ method, body string }{
		{http.MethodPost, `{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z"}`},
		{http.MethodGet, ""},
	} {
		rec := f.do(tc.method, "/conversations/whatsapp/entry-1/scheduled-messages", tc.body, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", tc.method, rec.Code)
		}
	}
}

func TestCreateRejectsAnUnknownEntryType(t *testing.T) {
	f := newHandlerFixture(true)

	rec := f.do(http.MethodPost, "/conversations/carrier-pigeon/entry-1/scheduled-messages",
		`{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z"}`, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// The composer needs the window with the list, so it never has to make two
// requests that can disagree.
func TestListCarriesTheWindow(t *testing.T) {
	f := newHandlerFixture(true)
	f.list.entryResult = &sm.ListForEntryResult{
		Messages: []*sm.ScheduledMessage{message()},
		Window:   openWindow(),
	}

	rec := f.do(http.MethodGet, "/conversations/whatsapp/entry-1/scheduled-messages", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		ScheduledMessages []ScheduledMessageResponse `json:"scheduledMessages"`
		Window            WindowResponse             `json:"window"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unparseable body: %v", err)
	}
	if len(body.ScheduledMessages) != 1 {
		t.Fatalf("messages = %d", len(body.ScheduledMessages))
	}
	if !body.Window.Open || body.Window.LatestAllowedAt == nil {
		t.Errorf("window = %+v", body.Window)
	}
}

func TestCancelReturns204(t *testing.T) {
	f := newHandlerFixture(true)

	rec := f.do(http.MethodDelete, "/scheduled-messages/sched-1", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// Cancelling something already sent is a conflict, not a success: the customer
// has it.
func TestCancelReturns409ForATerminalMessage(t *testing.T) {
	f := newHandlerFixture(true)
	f.cancel.err = sm.ErrNotPending

	rec := f.do(http.MethodDelete, "/scheduled-messages/sched-1", "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestRescheduleReturns200(t *testing.T) {
	f := newHandlerFixture(true)
	f.reschedule.result = &sm.ScheduleResult{Message: message(), Window: openWindow()}

	rec := f.do(http.MethodPatch, "/scheduled-messages/sched-1", `{"scheduled_at":"2026-08-12T16:00:00Z"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUnexpectedErrorsDoNotLeakInternals(t *testing.T) {
	f := newHandlerFixture(true)
	f.cancel.err = errors.New("pq: relation \"scheduled_messages\" does not exist")

	rec := f.do(http.MethodDelete, "/scheduled-messages/sched-1", "", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "relation") {
		t.Errorf("the database error reached the client: %s", rec.Body.String())
	}
}

func TestParseStatusesDropsUnknownValues(t *testing.T) {
	got := parseStatuses("pending, sent ,made-up,")
	if len(got) != 2 || got[0] != sm.StatusPending || got[1] != sm.StatusSent {
		t.Fatalf("statuses = %v, want the two known ones", got)
	}
	if parseStatuses("  ") != nil {
		t.Error("an empty filter should mean no filter, not an empty match")
	}
}
