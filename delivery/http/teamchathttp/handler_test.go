package teamchathttp

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
	"vozko/domain/shared"
	"vozko/domain/teamchat"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type call struct {
	name     string
	by       shared.Person
	ws       string
	threadID string
	id       string
	query    teamchat.MessageQuery
	cursor   string
	limit    int
	draft    teamchat.Draft
	ids      []string
	title    string
	seq      int64
	muted    bool
	sendAt   time.Time
	change   teamchat.Reschedule
}

type fakeChat struct {
	calls []call
	err   error
	sent  teamchat.Sent
}

func (f *fakeChat) record(c call) { f.calls = append(f.calls, c) }

func (f *fakeChat) last() call { return f.calls[len(f.calls)-1] }

var view = teamchat.ThreadView{
	Thread:  teamchat.Thread{ID: "t-1", Kind: teamchat.KindGroup, Title: "Vendas", LastSeq: 3},
	Me:      teamchat.Member{Role: teamchat.RoleAdmin, LastReadSeq: 2},
	Members: []teamchat.Participant{{Person: teamchat.Person{UserID: "u-1", Name: "Ana"}, Role: teamchat.RoleAdmin}},
	Unread:  1,
}

var message = teamchat.AuthoredMessage{
	Message:    teamchat.Message{ID: "m-1", ThreadID: "t-1", Seq: 3, AuthorID: "u-1", Kind: teamchat.MessageText, Body: "oi"},
	AuthorName: "Ana",
}

func (f *fakeChat) Directory(_ context.Context, by shared.Person, ws string) ([]teamchat.Person, error) {
	f.record(call{name: "directory", by: by, ws: ws})
	return []teamchat.Person{{UserID: "u-2", Name: "Bia"}}, f.err
}

func (f *fakeChat) Summary(_ context.Context, by shared.Person, ws string) (teamchat.Totals, error) {
	f.record(call{name: "summary", by: by, ws: ws})
	return teamchat.Totals{UnreadThreads: 2, UnreadMessages: 5}, f.err
}

func (f *fakeChat) Threads(_ context.Context, by shared.Person, ws, cursor string, limit int) (teamchat.ThreadPage, error) {
	f.record(call{name: "threads", by: by, ws: ws, cursor: cursor, limit: limit})
	return teamchat.ThreadPage{Threads: []teamchat.ThreadView{view}, Next: "next-cursor"}, f.err
}

func (f *fakeChat) Thread(_ context.Context, by shared.Person, ws, id string) (teamchat.ThreadView, error) {
	f.record(call{name: "thread", by: by, ws: ws, threadID: id})
	return view, f.err
}

func (f *fakeChat) Messages(_ context.Context, by shared.Person, ws string, q teamchat.MessageQuery) (teamchat.MessagePage, error) {
	f.record(call{name: "messages", by: by, ws: ws, query: q})
	return teamchat.MessagePage{Messages: []teamchat.AuthoredMessage{message}, HasMore: true}, f.err
}

func (f *fakeChat) OpenDirect(_ context.Context, by shared.Person, ws, other string) (teamchat.ThreadView, error) {
	f.record(call{name: "direct", by: by, ws: ws, id: other})
	return view, f.err
}

func (f *fakeChat) CreateGroup(_ context.Context, by shared.Person, ws, title string, ids []string) (teamchat.ThreadView, error) {
	f.record(call{name: "group", by: by, ws: ws, title: title, ids: ids})
	return view, f.err
}

func (f *fakeChat) Rename(_ context.Context, by shared.Person, ws, id, title string) (teamchat.ThreadView, error) {
	f.record(call{name: "rename", by: by, ws: ws, threadID: id, title: title})
	return view, f.err
}

func (f *fakeChat) AddMembers(_ context.Context, by shared.Person, ws, id string, ids []string) (teamchat.ThreadView, error) {
	f.record(call{name: "add", by: by, ws: ws, threadID: id, ids: ids})
	return view, f.err
}

func (f *fakeChat) RemoveMember(_ context.Context, by shared.Person, ws, id, userID string) error {
	f.record(call{name: "remove", by: by, ws: ws, threadID: id, id: userID})
	return f.err
}

func (f *fakeChat) Leave(_ context.Context, by shared.Person, ws, id string) error {
	f.record(call{name: "leave", by: by, ws: ws, threadID: id})
	return f.err
}

func (f *fakeChat) Mute(_ context.Context, by shared.Person, ws, id string, muted bool) error {
	f.record(call{name: "mute", by: by, ws: ws, threadID: id, muted: muted})
	return f.err
}

func (f *fakeChat) Send(_ context.Context, by shared.Person, ws, id string, d teamchat.Draft) (teamchat.Sent, error) {
	f.record(call{name: "send", by: by, ws: ws, threadID: id, draft: d})
	return f.sent, f.err
}

func (f *fakeChat) Edit(_ context.Context, by shared.Person, ws, id, body string) (teamchat.AuthoredMessage, error) {
	f.record(call{name: "edit", by: by, ws: ws, id: id, draft: teamchat.Draft{Body: body}})
	return message, f.err
}

func (f *fakeChat) Delete(_ context.Context, by shared.Person, ws, id string) (teamchat.AuthoredMessage, error) {
	f.record(call{name: "delete", by: by, ws: ws, id: id})
	return message, f.err
}

func (f *fakeChat) MarkRead(_ context.Context, by shared.Person, ws, id string, seq int64) (int64, error) {
	f.record(call{name: "read", by: by, ws: ws, threadID: id, seq: seq})
	return seq, f.err
}

func (f *fakeChat) Typing(_ context.Context, by shared.Person, ws, id string) error {
	f.record(call{name: "typing", by: by, ws: ws, threadID: id})
	return f.err
}

func (f *fakeChat) Schedule(_ context.Context, by shared.Person, ws, id string, d teamchat.Draft, at time.Time) (teamchat.Scheduled, error) {
	f.record(call{name: "schedule", by: by, ws: ws, threadID: id, draft: d, sendAt: at})
	return teamchat.Scheduled{ID: "s-1", ThreadID: id, Body: d.Body, SendAt: at, Status: teamchat.ScheduledPending}, f.err
}

func (f *fakeChat) Scheduled(_ context.Context, by shared.Person, ws, id string) ([]teamchat.Scheduled, error) {
	f.record(call{name: "scheduled", by: by, ws: ws, threadID: id})
	return nil, f.err
}

func (f *fakeChat) Reschedule(_ context.Context, by shared.Person, ws, id string, change teamchat.Reschedule) (teamchat.Scheduled, error) {
	f.record(call{name: "reschedule", by: by, ws: ws, id: id, change: change})
	return teamchat.Scheduled{ID: id, Status: teamchat.ScheduledPending}, f.err
}

func (f *fakeChat) CancelScheduled(_ context.Context, by shared.Person, ws, id string) error {
	f.record(call{name: "cancel", by: by, ws: ws, id: id})
	return f.err
}

type guard struct{ checked map[string]string }

func (g *guard) ac(resource workspace.Resource, action workspace.Action, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		route := mux.CurrentRoute(r)
		template, _ := route.GetPathTemplate()
		g.checked[r.Method+" "+template] = string(resource) + ":" + string(action)
		next(w, r)
	}
}

func serve(t *testing.T, chat *fakeChat, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterRoutes(router, NewHandler(chat), (&guard{checked: map[string]string{}}).ac)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "member"}))
	req = req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestOnlyGroupCreationIsGuardedByARolePermission(t *testing.T) {
	routes := []string{
		"GET /team-chat/people", "GET /team-chat/summary", "GET /team-chat/threads", "POST /team-chat/direct", "POST /team-chat/groups",
		"GET /team-chat/threads/{id}", "PATCH /team-chat/threads/{id}", "POST /team-chat/threads/{id}/members",
		"DELETE /team-chat/threads/{id}/members/{userId}", "POST /team-chat/threads/{id}/leave", "PUT /team-chat/threads/{id}/mute",
		"GET /team-chat/threads/{id}/messages", "POST /team-chat/threads/{id}/messages", "POST /team-chat/threads/{id}/read",
		"PATCH /team-chat/messages/{id}", "DELETE /team-chat/messages/{id}", "GET /team-chat/threads/{id}/scheduled",
		"POST /team-chat/threads/{id}/scheduled", "PATCH /team-chat/scheduled/{id}", "DELETE /team-chat/scheduled/{id}",
	}
	g := &guard{checked: map[string]string{}}
	router := mux.NewRouter()
	RegisterRoutes(router, NewHandler(&fakeChat{}), g.ac)
	for _, route := range routes {
		method, template, _ := strings.Cut(route, " ")
		path := strings.NewReplacer("{id}", "x", "{userId}", "y").Replace(template)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader("{}")))
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s is not registered: %d", route, rec.Code)
		}
	}
	want := map[string]string{"POST /team-chat/groups": "team_chat:create"}
	if len(g.checked) != len(want) || g.checked["POST /team-chat/groups"] != want["POST /team-chat/groups"] {
		t.Fatalf("guarded routes = %v, want only group creation behind team_chat:create", g.checked)
	}
}

func TestThreadsPassTheCursorAndLimit(t *testing.T) {
	chat := &fakeChat{}
	rec := serve(t, chat, http.MethodGet, "/team-chat/threads?cursor=abc&limit=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	got := chat.last()
	if got.cursor != "abc" || got.limit != 10 || got.ws != "ws-1" || got.by.UserID != "u-1" {
		t.Fatalf("call = %+v", got)
	}
	var body ThreadListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Threads) != 1 || body.Threads[0].Title != "Vendas" || body.Next != "next-cursor" || body.Threads[0].Me.Role != "admin" {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestMessagesParseTheSequenceWindow(t *testing.T) {
	chat := &fakeChat{}
	rec := serve(t, chat, http.MethodGet, "/team-chat/threads/t-1/messages?after_seq=7&limit=20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	q := chat.last().query
	if q.ThreadID != "t-1" || q.AfterSeq != 7 || q.BeforeSeq != 0 || q.Limit != 20 {
		t.Fatalf("query = %+v", q)
	}
	if !strings.Contains(rec.Body.String(), `"has_more":true`) || !strings.Contains(rec.Body.String(), `"author_name":"Ana"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestUnreadableQueryValuesAreRefused(t *testing.T) {
	cases := []struct{ path, code string }{
		{"/team-chat/threads?limit=muitos", "invalid_limit"},
		{"/team-chat/threads/t-1/messages?before_seq=-2", "invalid_seq"},
		{"/team-chat/threads/t-1/messages?after_seq=x", "invalid_seq"},
	}
	for _, c := range cases {
		rec := serve(t, &fakeChat{}, http.MethodGet, c.path, "")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%s: status = %d body = %s", c.path, rec.Code, rec.Body)
		}
	}
}

func TestSendAnswersCreatedOnlyForANewMessage(t *testing.T) {
	chat := &fakeChat{sent: teamchat.Sent{Message: message, Created: true}}
	rec := serve(t, chat, http.MethodPost, "/team-chat/threads/t-1/messages", `{"body":"oi","mentions":["u-2"],"reply_to_id":"m-0","client_id":"c-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	d := chat.last().draft
	if d.Body != "oi" || d.ClientID != "c-1" || d.ReplyToID != "m-0" || len(d.Mentions) != 1 {
		t.Fatalf("draft = %+v", d)
	}
	chat.sent.Created = false
	if rec := serve(t, chat, http.MethodPost, "/team-chat/threads/t-1/messages", `{"body":"oi","client_id":"c-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("a repeated client_id answers %d, want 200", rec.Code)
	}
	if rec := serve(t, chat, http.MethodPost, "/team-chat/threads/t-1/messages", `{"body":"oi","extra":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field answers %d", rec.Code)
	}
}

func TestScheduleParsesSendAt(t *testing.T) {
	chat := &fakeChat{}
	rec := serve(t, chat, http.MethodPost, "/team-chat/threads/t-1/scheduled", `{"body":"lembrete","send_at":"2026-10-20T15:00:00Z"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if !chat.last().sendAt.Equal(time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("send_at = %v", chat.last().sendAt)
	}
	if rec := serve(t, chat, http.MethodPost, "/team-chat/threads/t-1/scheduled", `{"body":"x"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "send_at_required") {
		t.Fatalf("missing send_at: %d %s", rec.Code, rec.Body)
	}
	at := "2026-10-21T10:00:00Z"
	rec = serve(t, chat, http.MethodPatch, "/team-chat/scheduled/s-1", `{"send_at":"`+at+`"}`)
	if rec.Code != http.StatusOK || chat.last().change.SendAt == nil || chat.last().change.Body != nil {
		t.Fatalf("reschedule: %d %+v", rec.Code, chat.last().change)
	}
}

func TestMarkReadAndMute(t *testing.T) {
	chat := &fakeChat{}
	rec := serve(t, chat, http.MethodPost, "/team-chat/threads/t-1/read", `{"seq":9}`)
	if rec.Code != http.StatusOK || chat.last().seq != 9 || !strings.Contains(rec.Body.String(), `"last_read_seq":9`) {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	rec = serve(t, chat, http.MethodPut, "/team-chat/threads/t-1/mute", `{"muted":true}`)
	if rec.Code != http.StatusNoContent || !chat.last().muted {
		t.Fatalf("mute: %d %+v", rec.Code, chat.last())
	}
}

func TestDomainErrorsMapToStableCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{teamchat.ErrForbidden, http.StatusForbidden, "forbidden"},
		{teamchat.ErrNotFound, http.StatusNotFound, "thread_not_found"},
		{teamchat.ErrMessageNotFound, http.StatusNotFound, "message_not_found"},
		{teamchat.ErrScheduledNotFound, http.StatusNotFound, "scheduled_not_found"},
		{teamchat.ErrScheduledFinal, http.StatusConflict, "scheduled_final"},
		{teamchat.ErrNotAuthor, http.StatusForbidden, "not_author"},
		{teamchat.ErrNotAdmin, http.StatusForbidden, "not_admin"},
		{teamchat.ErrDirectThread, http.StatusConflict, "direct_thread"},
		{teamchat.ErrRecipientUnavailable, http.StatusConflict, "recipient_unavailable"},
		{teamchat.ErrMemberUnavailable, http.StatusConflict, "member_unavailable"},
		{teamchat.ErrBodyRequired, http.StatusBadRequest, "body_required"},
		{teamchat.ErrBodyTooLong, http.StatusBadRequest, "body_too_long"},
		{teamchat.ErrSendAtTooSoon, http.StatusBadRequest, "send_at_too_soon"},
		{shared.ErrKeysetInvalid, http.StatusBadRequest, "invalid_cursor"},
		{errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		rec := serve(t, &fakeChat{err: c.err}, http.MethodGet, "/team-chat/summary", "")
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%v: status = %d body = %s", c.err, rec.Code, rec.Body)
		}
	}
}
