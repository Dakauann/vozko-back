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
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
	scheduled_message_usecase "vozko/usecases/scheduled_message"
)

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

type stubMessages struct{ sm.Repository }

func (stubMessages) FindByID(id string) (*sm.ScheduledMessage, error) {
	return &sm.ScheduledMessage{ID: id, WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}, nil
}

type stubPermissions struct{ err error }

func (p *stubPermissions) Execute(string, string, workspace.Resource, workspace.Action) error {
	return p.err
}

type handlerFixture struct {
	schedule    *stubSchedule
	reschedule  *stubReschedule
	cancel      *stubCancel
	list        *stubList
	permissions *stubPermissions
	router      *mux.Router
}

func newHandlerFixture(allowAccess bool) *handlerFixture {
	f := &handlerFixture{
		schedule:    &stubSchedule{},
		reschedule:  &stubReschedule{},
		cancel:      &stubCancel{},
		list:        &stubList{},
		permissions: &stubPermissions{},
	}
	access := stubAuthorizer{allow: allowAccess}
	scheduler := scheduled_message_usecase.NewPersonSchedulerUseCase(access, f.permissions, stubMessages{}, f.schedule, f.reschedule, f.cancel)
	h := NewScheduledMessageHandler(scheduler, f.list, access)

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
		{"template with free text", sm.ErrTemplateWithFreeContent, http.StatusBadRequest, "invalid_request", false},
		{"channel without templates", sm.ErrTemplatesUnsupported, http.StatusUnprocessableEntity, "templates_unsupported", false},
		{"template variable missing", template.ErrTemplateParamsMismatch, http.StatusUnprocessableEntity, "template_params", false},
		{"template paused", template.ErrTemplateNotSendable, http.StatusUnprocessableEntity, "template_unavailable", false},
		{"template of another account", template.ErrTemplatePhoneMismatch, http.StatusUnprocessableEntity, "template_unavailable", false},
		{"template not granted", wo.ErrTemplateForbidden, http.StatusUnprocessableEntity, "template_unavailable", false},
		{"template deleted", wo.ErrTemplateNotFound, http.StatusUnprocessableEntity, "template_unavailable", false},
		{"blocked contact", wo.ErrLeadBlocked, http.StatusUnprocessableEntity, "contact_blocked", false},
		{"inside the spam window", wo.ErrWithinSpamWindow, http.StatusUnprocessableEntity, "spam_window", false},
		{"number disconnected", wo.ErrPhoneNotConnected, http.StatusUnprocessableEntity, "number_unavailable", false},
		{"number withdrawn", wo.ErrBusinessPhoneNotFound, http.StatusUnprocessableEntity, "number_unavailable", false},
		{"conversation gone", wo.ErrConversationNotFound, http.StatusNotFound, "not_found", false},
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
	for _, method := range []string{http.MethodDelete, http.MethodPatch} {
		rec := f.do(method, "/scheduled-messages/sched-1", `{"scheduled_at":"2026-08-12T14:00:00Z"}`, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", method, rec.Code)
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

func templateMessageResponse() *sm.ScheduledMessage {
	m := message()
	m.Text = ""
	m.Kind = sm.KindTemplate
	m.Template = &sm.TemplateContent{ID: "tpl-1", Name: "follow_up", Preview: "Oi Ana", BodyParams: []string{"Ana"}}
	return m
}

func TestCreateForwardsATemplate(t *testing.T) {
	f := newHandlerFixture(true)
	f.schedule.result = &sm.ScheduleResult{Message: templateMessageResponse(), Window: openWindow()}

	rec := f.do(http.MethodPost, "/conversations/whatsapp/entry-1/scheduled-messages",
		`{"scheduled_at":"2026-08-14T14:00:00Z","template":{"template_id":"tpl-1","body_params":["Ana"],"header_params":["42"]}}`, nil)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	in := f.schedule.last
	if in.Template == nil || in.Template.ID != "tpl-1" {
		t.Fatalf("template = %+v, want it forwarded", in.Template)
	}
	if len(in.Template.BodyParams) != 1 || in.Template.BodyParams[0] != "Ana" || in.Template.HeaderParams[0] != "42" {
		t.Errorf("values = %+v", in.Template)
	}

	var body struct {
		ScheduledMessage struct {
			Kind     string `json:"kind"`
			Template struct {
				Name    string `json:"name"`
				Preview string `json:"preview"`
			} `json:"template"`
		} `json:"scheduledMessage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ScheduledMessage.Kind != "template" || body.ScheduledMessage.Template.Name != "follow_up" {
		t.Errorf("response = %s", rec.Body.String())
	}
}

func TestATextCreateCarriesNoTemplate(t *testing.T) {
	f := newHandlerFixture(true)
	f.schedule.result = &sm.ScheduleResult{Message: message(), Window: openWindow()}

	f.do(http.MethodPost, "/conversations/whatsapp/entry-1/scheduled-messages",
		`{"text":"oi","scheduled_at":"2026-08-12T14:00:00Z"}`, nil)

	if f.schedule.last.Template != nil {
		t.Fatalf("a text schedule was turned into a template: %+v", f.schedule.last.Template)
	}
}

func TestCreateATemplateWithoutTheTemplatePermission(t *testing.T) {
	f := newHandlerFixture(true)
	f.permissions.err = workspace.ErrInsufficientPermissions

	rec := f.do(http.MethodPost, "/conversations/whatsapp/entry-1/scheduled-messages",
		`{"scheduled_at":"2026-08-14T14:00:00Z","template":{"template_id":"tpl-1","body_params":["Ana"]}}`, nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "template_forbidden") {
		t.Errorf("body = %s, want the template_forbidden code", rec.Body.String())
	}
	if f.schedule.last.Template != nil {
		t.Error("the use case ran for someone without the template permission")
	}
}
