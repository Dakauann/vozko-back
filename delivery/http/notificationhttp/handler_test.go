package notificationhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/notification"
	"vozko/infra/http/middleware"
	notification_usecase "vozko/usecases/notification"
)

type fakeInbox struct {
	viewer   notification.Viewer
	query    notification.ListQuery
	ids      []string
	category string
	change   notification_usecase.PreferencesChange
	err      error
}

var at = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func (f *fakeInbox) List(_ context.Context, v notification.Viewer, q notification.ListQuery) (notification.Page, error) {
	f.viewer, f.query = v, q
	return notification.Page{Items: []notification.Item{{ID: "n-1", Category: notification.CategoryCampaigns, Severity: notification.SeverityWarning, Title: "Pausada", CreatedAt: at}}, Next: "cursor-2"}, f.err
}

func (f *fakeInbox) Summary(_ context.Context, v notification.Viewer) (notification.Summary, error) {
	f.viewer = v
	return notification.Summary{Unread: 3, ByCategory: map[notification.Category]int{notification.CategoryCampaigns: 3}}, f.err
}

func (f *fakeInbox) MarkRead(_ context.Context, v notification.Viewer, ids []string) (int, error) {
	f.viewer, f.ids = v, ids
	return len(ids), f.err
}

func (f *fakeInbox) MarkUnread(_ context.Context, v notification.Viewer, ids []string) (int, error) {
	f.viewer, f.ids = v, ids
	return len(ids), f.err
}

func (f *fakeInbox) MarkAllRead(_ context.Context, v notification.Viewer, category string) (int, error) {
	f.viewer, f.category = v, category
	return 7, f.err
}

func (f *fakeInbox) Preferences(_ context.Context, v notification.Viewer) (notification.Preferences, error) {
	f.viewer = v
	return notification.Preferences{EmailMuted: []notification.Category{notification.CategoryBilling}}, f.err
}

func (f *fakeInbox) SavePreferences(_ context.Context, v notification.Viewer, change notification_usecase.PreferencesChange) (notification.Preferences, error) {
	f.viewer, f.change = v, change
	return notification.Preferences{EmailMuted: []notification.Category{notification.CategoryTeam}, EmailDigest: change.EmailDigest}, f.err
}

func call(t *testing.T, h *Handler, method, target, body, workspaceID string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, h)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	if workspaceID != "" {
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, workspaceID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, into interface{}) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func TestListReadsTheViewerFromTheSessionAndTheFiltersFromTheQuery(t *testing.T) {
	inbox := &fakeInbox{}
	rec := call(t, NewHandler(inbox), http.MethodGet, "/notifications?category=campaigns&unread=true&limit=5", "", "ws-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	if inbox.viewer != (notification.Viewer{WorkspaceID: "ws-1", UserID: "user-1"}) {
		t.Fatalf("viewer = %+v", inbox.viewer)
	}
	if inbox.query.Category != notification.CategoryCampaigns || !inbox.query.UnreadOnly || inbox.query.Limit != 5 {
		t.Fatalf("query = %+v", inbox.query)
	}
	var page ListResponse
	decode(t, rec, &page)
	if len(page.Items) != 1 || page.Items[0].ID != "n-1" || page.Items[0].Severity != "warning" || page.Next != "cursor-2" {
		t.Fatalf("page = %+v", page)
	}
}

func TestListRefusesBadFiltersWithCodes(t *testing.T) {
	cases := map[string]string{
		"/notifications?category=misc":    "notification_category_unknown",
		"/notifications?cursor=%21%21":    "notification_cursor_invalid",
		"/notifications?limit=many":       "notification_limit_invalid",
		"/notifications?unread=sometimes": "notification_unread_invalid",
	}
	for target, code := range cases {
		rec := call(t, NewHandler(&fakeInbox{}), http.MethodGet, target, "", "ws-1")
		var body struct{ Code string }
		decode(t, rec, &body)
		if rec.Code != http.StatusBadRequest || body.Code != code {
			t.Fatalf("%s = %d %q, want 400 %q", target, rec.Code, body.Code, code)
		}
	}
}

func TestEveryRouteNeedsAWorkspace(t *testing.T) {
	for _, r := range []struct{ method, target, body string }{
		{http.MethodGet, "/notifications", ""},
		{http.MethodGet, "/notifications/summary", ""},
		{http.MethodPost, "/notifications/read", `{"ids":["n-1"]}`},
		{http.MethodPost, "/notifications/unread", `{"ids":["n-1"]}`},
		{http.MethodPost, "/notifications/read-all", `{}`},
		{http.MethodGet, "/notifications/preferences", ""},
		{http.MethodPut, "/notifications/preferences", `{"emailMuted":[]}`},
	} {
		if rec := call(t, NewHandler(&fakeInbox{}), r.method, r.target, r.body, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s without workspace = %d", r.method, r.target, rec.Code)
		}
	}
}

func TestSummaryAndMarks(t *testing.T) {
	inbox := &fakeInbox{}
	h := NewHandler(inbox)

	var summary SummaryResponse
	decode(t, call(t, h, http.MethodGet, "/notifications/summary", "", "ws-1"), &summary)
	if summary.Unread != 3 || summary.ByCategory["campaigns"] != 3 {
		t.Fatalf("summary = %+v", summary)
	}

	var updated ChangeResponse
	decode(t, call(t, h, http.MethodPost, "/notifications/read", `{"ids":["n-1","n-2"]}`, "ws-1"), &updated)
	if updated.Updated != 2 || len(inbox.ids) != 2 {
		t.Fatalf("read = %+v ids=%v", updated, inbox.ids)
	}
	decode(t, call(t, h, http.MethodPost, "/notifications/unread", `{"ids":["n-3"]}`, "ws-1"), &updated)
	if updated.Updated != 1 || inbox.ids[0] != "n-3" {
		t.Fatalf("unread = %+v", updated)
	}
	decode(t, call(t, h, http.MethodPost, "/notifications/read-all", `{"category":"billing"}`, "ws-1"), &updated)
	if updated.Updated != 7 || inbox.category != "billing" {
		t.Fatalf("read all = %+v category=%q", updated, inbox.category)
	}
	if rec := call(t, h, http.MethodPost, "/notifications/read-all", ``, "ws-1"); rec.Code != http.StatusOK || inbox.category != "" {
		t.Fatalf("read all without a body = %d category=%q", rec.Code, inbox.category)
	}
	if rec := call(t, h, http.MethodPost, "/notifications/read", `{"ids":["n-1"],"extra":1}`, "ws-1"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown fields must be refused, got %d", rec.Code)
	}
}

func TestDomainRefusalsMapToStatusAndCode(t *testing.T) {
	for err, status := range map[error]int{
		notification.ErrIDsInvalid:        http.StatusBadRequest,
		notification.ErrCategoryUnknown:   http.StatusBadRequest,
		notification.ErrViewerRequired:    http.StatusUnauthorized,
		notification.ErrWorkspaceRequired: http.StatusForbidden,
	} {
		rec := call(t, NewHandler(&fakeInbox{err: err}), http.MethodPost, "/notifications/read", `{"ids":[]}`, "ws-1")
		var body struct{ Code string }
		decode(t, rec, &body)
		if rec.Code != status || body.Code != notification.ErrorCode(err) {
			t.Fatalf("%v = %d %q", err, rec.Code, body.Code)
		}
	}
	rec := call(t, NewHandler(&fakeInbox{err: context.DeadlineExceeded}), http.MethodGet, "/notifications/summary", "", "ws-1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("unknown errors = %d", rec.Code)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	inbox := &fakeInbox{}
	h := NewHandler(inbox)
	var prefs PreferencesResponse
	decode(t, call(t, h, http.MethodGet, "/notifications/preferences", "", "ws-1"), &prefs)
	if len(prefs.Categories) != len(notification.Categories()) || prefs.EmailMuted[0] != "billing" {
		t.Fatalf("prefs = %+v", prefs)
	}
	decode(t, call(t, h, http.MethodPut, "/notifications/preferences", `{"emailMuted":["team"],"emailDigest":true}`, "ws-1"), &prefs)
	if prefs.EmailMuted[0] != "team" || !prefs.EmailDigest || inbox.change.EmailMuted[0] != "team" || !inbox.change.EmailDigest {
		t.Fatalf("saved = %+v sent=%+v", prefs, inbox.change)
	}
}

type fakeAdmin struct {
	adminID string
	draft   notification.BroadcastDraft
	cursor  string
	upload  []byte
	err     error
}

func (f *fakeAdmin) Create(_ context.Context, adminID string, d notification.BroadcastDraft) (notification.Broadcast, error) {
	f.adminID, f.draft = adminID, d
	return notification.Broadcast{ID: "b-1", BroadcastDraft: d, CreatedBy: adminID, Status: notification.BroadcastQueued, CreatedAt: at}, f.err
}

func (f *fakeAdmin) List(_ context.Context, cursor string, _ int) (notification.BroadcastPage, error) {
	f.cursor = cursor
	return notification.BroadcastPage{Items: []notification.Broadcast{{ID: "b-1", Status: notification.BroadcastCompleted, WorkspaceCount: 4, CreatedAt: at, Stats: notification.BroadcastStats{Recipients: 7, Read: 3}}}}, f.err
}

func (f *fakeAdmin) Upload(_ context.Context, data []byte) (string, error) {
	f.upload = data
	return "https://cdn/notifications/images/a.png", f.err
}

func adminCall(t *testing.T, a *fakeAdmin, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterAdminRoutes(router, NewAdminHandler(a, a))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "admin-1", Role: "admin"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestAdminCreatesABroadcastFromTheBody(t *testing.T) {
	admin := &fakeAdmin{}
	body := `{"target":{"workspaceIds":["ws-1"]},"audience":{"kind":"permission","resource":"whatsapp_campaigns","action":"update"},` +
		`"category":"announcements","severity":"info","title":"Novidade","body":"Chegou","actionUrl":"/dashboard","actionLabel":"Ver","email":true}`
	rec := adminCall(t, admin, httptest.NewRequest(http.MethodPost, "/admin/notifications/broadcasts", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	d := admin.draft
	if admin.adminID != "admin-1" || d.Target.WorkspaceIDs[0] != "ws-1" || d.Audience.Kind != notification.AudiencePermission ||
		d.Audience.Permission == nil || d.Audience.Permission.Resource != "whatsapp_campaigns" || !d.Email || d.ActionLabel != "Ver" {
		t.Fatalf("draft = %+v", d)
	}
	var out BroadcastResponse
	decode(t, rec, &out)
	if out.ID != "b-1" || out.Status != "queued" {
		t.Fatalf("response = %+v", out)
	}

	admin.err = notification.ErrTargetInvalid
	rec = adminCall(t, admin, httptest.NewRequest(http.MethodPost, "/admin/notifications/broadcasts", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("refusal = %d", rec.Code)
	}
}

func TestAdminListsBroadcasts(t *testing.T) {
	admin := &fakeAdmin{}
	rec := adminCall(t, admin, httptest.NewRequest(http.MethodGet, "/admin/notifications/broadcasts?cursor=abc", nil))
	var out BroadcastListResponse
	decode(t, rec, &out)
	if admin.cursor != "abc" || len(out.Items) != 1 || out.Items[0].WorkspaceCount != 4 {
		t.Fatalf("list = %+v", out)
	}
}

func TestAdminUploadsAnImageFromTheFileField(t *testing.T) {
	admin := &fakeAdmin{}
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, _ := form.CreateFormFile("file", "a.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nbytes"))
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/notifications/images", &buf)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := adminCall(t, admin, req)
	var out ImageResponse
	decode(t, rec, &out)
	if rec.Code != http.StatusCreated || out.URL == "" || !bytes.HasPrefix(admin.upload, []byte("\x89PNG")) {
		t.Fatalf("upload = %d %+v", rec.Code, out)
	}

	missing := httptest.NewRequest(http.MethodPost, "/admin/notifications/images", strings.NewReader("nothing"))
	missing.Header.Set("Content-Type", "text/plain")
	if rec := adminCall(t, admin, missing); rec.Code != http.StatusBadRequest {
		t.Fatalf("no file = %d", rec.Code)
	}
}

func (f *fakeAdmin) Preview(_ context.Context, d notification.BroadcastDraft) (notification.BroadcastReach, error) {
	f.draft = d
	return notification.BroadcastReach{Workspaces: 2, Recipients: 5, People: 4, Emails: 3}, f.err
}

func (f *fakeAdmin) Recipients(_ context.Context, broadcastID, status, cursor string, _ int) (notification.RecipientPage, error) {
	f.cursor = broadcastID + "|" + status + "|" + cursor
	return notification.RecipientPage{Items: []notification.BroadcastRecipient{
		{UserID: "u-1", Name: "Ana", Email: "ana@x.com", WorkspaceID: "ws-1", WorkspaceName: "Loja", ReadAt: &at},
		{UserID: "u-2", Name: "Beto", Email: "beto@x.com", WorkspaceID: "ws-1", WorkspaceName: "Loja"},
	}, Next: "next-page"}, f.err
}

func TestAdminPreviewsTheReachOfADraftWithoutCreatingIt(t *testing.T) {
	admin := &fakeAdmin{}
	body := `{"target":{"all":true},"audience":{"kind":"managers"},"category":"announcements","severity":"info","title":"Novidade","body":"","email":true}`
	rec := adminCall(t, admin, httptest.NewRequest(http.MethodPost, "/admin/notifications/broadcasts/preview", strings.NewReader(body)))
	var out ReachResponse
	decode(t, rec, &out)
	if rec.Code != http.StatusOK || out != (ReachResponse{Workspaces: 2, Recipients: 5, People: 4, Emails: 3}) || !admin.draft.Target.All {
		t.Fatalf("preview = %d %+v draft %+v", rec.Code, out, admin.draft)
	}
	admin.err = notification.ErrTitleRequired
	if rec := adminCall(t, admin, httptest.NewRequest(http.MethodPost, "/admin/notifications/broadcasts/preview", strings.NewReader(body))); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid draft = %d", rec.Code)
	}
}

func TestAdminListsWhoReceivedABroadcastAndWhoRead(t *testing.T) {
	admin := &fakeAdmin{}
	rec := adminCall(t, admin, httptest.NewRequest(http.MethodGet, "/admin/notifications/broadcasts/b-1/recipients?status=read&cursor=c", nil))
	var out RecipientListResponse
	decode(t, rec, &out)
	if rec.Code != http.StatusOK || admin.cursor != "b-1|read|c" || len(out.Items) != 2 || out.Next != "next-page" {
		t.Fatalf("recipients = %d %+v (%s)", rec.Code, out, admin.cursor)
	}
	if out.Items[0].Name != "Ana" || out.Items[0].WorkspaceName != "Loja" || out.Items[0].ReadAt == nil || out.Items[1].ReadAt != nil {
		t.Fatalf("items = %+v", out.Items)
	}
	admin.err = notification.ErrReadStatusUnknown
	rec = adminCall(t, admin, httptest.NewRequest(http.MethodGet, "/admin/notifications/broadcasts/b-1/recipients?status=seen", nil))
	var body struct{ Code string }
	decode(t, rec, &body)
	if rec.Code != http.StatusBadRequest || body.Code != "notification_status_invalid" {
		t.Fatalf("bad status = %d %q", rec.Code, body.Code)
	}
}

func TestAdminBroadcastListCarriesReadStats(t *testing.T) {
	admin := &fakeAdmin{}
	rec := adminCall(t, admin, httptest.NewRequest(http.MethodGet, "/admin/notifications/broadcasts", nil))
	var out BroadcastListResponse
	decode(t, rec, &out)
	if out.Items[0].Recipients != 7 || out.Items[0].Read != 3 {
		t.Fatalf("stats = %+v", out.Items[0])
	}
}
