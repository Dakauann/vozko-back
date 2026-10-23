package conversationnotehttp

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
	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type fakeNotes struct {
	listQuery conversationnote.PageQuery
	listBy    shared.Person
	page      conversationnote.Page
	draft     conversationnote.Draft
	created   conversationnote.Authored
	deleted   string
	upload    conversationnote.ImageUpload
	image     conversationnote.Image
	err       error
}

func (f *fakeNotes) List(_ context.Context, by shared.Person, q conversationnote.PageQuery) (conversationnote.Page, error) {
	f.listBy, f.listQuery = by, q
	return f.page, f.err
}

func (f *fakeNotes) Create(_ context.Context, _ shared.Person, d conversationnote.Draft) (conversationnote.Authored, error) {
	f.draft = d
	return f.created, f.err
}

func (f *fakeNotes) Delete(_ context.Context, _ shared.Person, workspaceID, noteID string) error {
	f.deleted = workspaceID + "/" + noteID
	return f.err
}

type guard struct{ checked []string }

func (g *guard) ac(resource workspace.Resource, action workspace.Action, next http.HandlerFunc) http.HandlerFunc {
	g.checked = append(g.checked, string(resource)+":"+string(action))
	return next
}

func serve(t *testing.T, notes *fakeNotes, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterRoutes(router, NewHandler(notes), (&guard{}).ac)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Workspace-ID", "ws-1")
	req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "member"}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var sampleNote = conversationnote.Authored{
	Note: conversationnote.Note{
		ID: "note-1", WorkspaceID: "ws-1", EntryType: shared.EntryTypeWhatsApp, EntryID: "entry-1",
		AuthorID: "u-1", Body: "interno", Mentions: []string{"u-2"}, CreatedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
	},
	AuthorName: "Carla",
}

func TestEveryNoteRouteIsGuardedByConversationsRead(t *testing.T) {
	g := &guard{}
	RegisterRoutes(mux.NewRouter(), NewHandler(&fakeNotes{}), g.ac)
	if len(g.checked) != 4 {
		t.Fatalf("guarded routes = %v, want 4", g.checked)
	}
	for _, c := range g.checked {
		if c != "conversations:read" {
			t.Fatalf("route guarded by %s, want conversations:read", c)
		}
	}
}

func TestListPassesTheCallerTheConversationAndTheCursor(t *testing.T) {
	notes := &fakeNotes{page: conversationnote.Page{Notes: []conversationnote.Authored{sampleNote}, HasMore: true}}

	rec := serve(t, notes, http.MethodGet, "/conversations/whatsapp/entry-1/notes?before=2026-10-18T12:00:00.5Z&limit=20", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	q := notes.listQuery
	if q.WorkspaceID != "ws-1" || q.Entry != (shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}) || q.Limit != 20 {
		t.Fatalf("query = %+v", q)
	}
	if q.Before == nil || !q.Before.Equal(time.Date(2026, 10, 18, 12, 0, 0, 500000000, time.UTC)) {
		t.Fatalf("before = %v", q.Before)
	}
	if notes.listBy.UserID != "u-1" {
		t.Fatalf("caller = %+v", notes.listBy)
	}
	var body struct {
		Notes   []map[string]any `json:"notes"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Notes) != 1 || body.Notes[0]["author_name"] != "Carla" || !body.HasMore {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestListAnswersAnEmptyArrayRatherThanNull(t *testing.T) {
	rec := serve(t, &fakeNotes{}, http.MethodGet, "/conversations/whatsapp/entry-1/notes", "")
	if !strings.Contains(rec.Body.String(), `"notes":[]`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestListRefusesUnreadableInput(t *testing.T) {
	cases := []struct {
		name, path, code string
	}{
		{"before is not a date", "/conversations/whatsapp/entry-1/notes?before=ontem", "invalid_before"},
		{"limit is not a number", "/conversations/whatsapp/entry-1/notes?limit=muitos", "invalid_limit"},
		{"limit below one", "/conversations/whatsapp/entry-1/notes?limit=0", "invalid_limit"},
		{"unknown channel", "/conversations/voice/entry-1/notes", "invalid_entry_type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notes := &fakeNotes{}
			rec := serve(t, notes, http.MethodGet, c.path, "")
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.code) {
				t.Fatalf("status = %d body = %s, want 400 %s", rec.Code, rec.Body, c.code)
			}
			if notes.listBy.UserID != "" {
				t.Fatal("the use case must not run on unreadable input")
			}
		})
	}
}

func TestCreateSendsTheBodyAndMentionsAndAnswersTheNote(t *testing.T) {
	notes := &fakeNotes{created: sampleNote}

	rec := serve(t, notes, http.MethodPost, "/conversations/whatsapp/entry-1/notes", `{"body":"interno","mentions":["u-2"]}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	d := notes.draft
	if d.WorkspaceID != "ws-1" || d.Entry.EntryID != "entry-1" || d.Body != "interno" || strings.Join(d.Mentions, ",") != "u-2" {
		t.Fatalf("draft = %+v", d)
	}
	var body struct {
		Note map[string]any `json:"note"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Note["id"] != "note-1" || body.Note["entry_type"] != "whatsapp" {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestCreateRefusesABodyThatIsNotJSON(t *testing.T) {
	rec := serve(t, &fakeNotes{}, http.MethodPost, "/conversations/whatsapp/entry-1/notes", `nota`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestDeleteNamesTheNoteInTheCallersWorkspace(t *testing.T) {
	notes := &fakeNotes{}
	rec := serve(t, notes, http.MethodDelete, "/conversation-notes/note-1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if notes.deleted != "ws-1/note-1" {
		t.Fatalf("deleted = %q", notes.deleted)
	}
}

func TestDomainRefusalsMapToStableCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{conversation.ErrUnauthorized, http.StatusForbidden, "forbidden"},
		{conversationnote.ErrNotFound, http.StatusNotFound, "not_found"},
		{conversationnote.ErrNotAuthor, http.StatusForbidden, "not_author"},
		{conversationnote.ErrBodyRequired, http.StatusBadRequest, "body_required"},
		{conversationnote.ErrBodyTooLong, http.StatusBadRequest, "body_too_long"},
		{conversationnote.ErrTooManyMentions, http.StatusBadRequest, "too_many_mentions"},
		{errors.New("db down"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		t.Run(c.err.Error(), func(t *testing.T) {
			rec := serve(t, &fakeNotes{err: c.err}, http.MethodPost, "/conversations/whatsapp/entry-1/notes", `{"body":"x"}`)
			if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.code) {
				t.Fatalf("status = %d body = %s, want %d %s", rec.Code, rec.Body, c.status, c.code)
			}
			if strings.Contains(rec.Body.String(), "db down") {
				t.Fatal("internal errors are never echoed")
			}
		})
	}
}

func (f *fakeNotes) AttachImage(_ context.Context, _ shared.Person, in conversationnote.ImageUpload) (conversationnote.Image, error) {
	f.upload = in
	return f.image, f.err
}
