package studiohttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/media"
	"vozko/domain/studio"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type fakeService struct {
	project *studio.Project
	saveErr error
	version int64
}

func (f *fakeService) Create(_ context.Context, ws, user string, kind studio.Kind, name string, doc json.RawMessage) (*studio.Project, error) {
	return &studio.Project{ID: "p-1", WorkspaceID: ws, CreatedBy: user, Kind: kind, Name: name, Document: doc, Version: 1}, nil
}

func (f *fakeService) List(context.Context, studio.ListQuery) ([]studio.Summary, int64, error) {
	return []studio.Summary{{ID: "p-1", Kind: studio.KindVideo, Name: "Reels", Version: 3}}, 1, nil
}

func (f *fakeService) Get(context.Context, string, string) (*studio.Project, error) {
	return f.project, nil
}

func (f *fakeService) Save(_ context.Context, _, _ string, version int64, _ studio.Change) (*studio.Project, error) {
	f.version = version
	return f.project, f.saveErr
}

func (f *fakeService) Archive(context.Context, string, string) error { return nil }

type fakeCapabilities struct {
	got studio.CapabilityReport
	err error
}

func (f *fakeCapabilities) Report(_ context.Context, report studio.CapabilityReport) error {
	f.got = report
	return f.err
}

type fakeExports struct {
	received []byte
	err      error
}

func (f *fakeExports) Save(_ context.Context, _, _ string, video io.Reader) (media.Media, error) {
	received, err := io.ReadAll(video)
	if err != nil {
		return media.Media{}, err
	}
	f.received = received
	return media.Media{ID: "e-1", URL: "https://cdn.test/e-1.mp4"}, f.err
}

func exportForm(t *testing.T, field string, video []byte) (string, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile(field, "export.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(video); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return body.String(), form.FormDataContentType()
}

func serve(t *testing.T, svc *fakeService, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	return serveWith(t, svc, &fakeCapabilities{}, method, path, body, headers)
}

func serveWith(t *testing.T, svc *fakeService, capabilities *fakeCapabilities, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	return serveAll(t, svc, capabilities, &fakeExports{}, method, path, body, headers)
}

func serveAll(t *testing.T, svc *fakeService, capabilities *fakeCapabilities, exports *fakeExports, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	allow := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return next
	}
	RegisterProtectedRoutes(router, NewHandler(svc, capabilities, exports), allow)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func current() *studio.Project {
	return &studio.Project{ID: "p-1", Kind: studio.KindVideo, Name: "Reels", Document: json.RawMessage(`{"schema":"studio.video"}`), Version: 4}
}

func TestSavingNeedsTheLoadedVersion(t *testing.T) {
	svc := &fakeService{project: current()}
	if rec := serve(t, svc, http.MethodPatch, "/studio/projects/p-1", `{"name":"x"}`, nil); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status %d", rec.Code)
	}
	rec := serve(t, svc, http.MethodPatch, "/studio/projects/p-1", `{"name":"x"}`, map[string]string{"If-Match": `"3"`})
	if rec.Code != http.StatusOK || svc.version != 3 {
		t.Fatalf("status %d version %d", rec.Code, svc.version)
	}
}

func TestAStaleSaveAnswersTheCurrentProject(t *testing.T) {
	svc := &fakeService{project: current(), saveErr: studio.ErrVersionConflict}
	rec := serve(t, svc, http.MethodPatch, "/studio/projects/p-1", `{"name":"x"}`, map[string]string{"If-Match": "3"})
	var body struct {
		Code    string          `json:"code"`
		Current ProjectResponse `json:"current"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusConflict || body.Code != "version_conflict" || body.Current.Version != 4 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAProjectBodyPastTheDocumentSizeIsRefusedBeforeTheService(t *testing.T) {
	svc := &fakeService{project: current()}
	huge := `{"kind":"image","name":"x","document":"` + strings.Repeat("a", studio.MaxDocumentBytes+projectEnvelopeBytes) + `"}`
	if rec := serve(t, svc, http.MethodPost, "/studio/projects", huge, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("create status %d", rec.Code)
	}
	if rec := serve(t, svc, http.MethodPatch, "/studio/projects/p-1", huge, map[string]string{"If-Match": "3"}); rec.Code != http.StatusBadRequest || svc.version != 0 {
		t.Fatalf("save status %d version %d", rec.Code, svc.version)
	}
}

func TestProjectsAreCreatedAndListed(t *testing.T) {
	svc := &fakeService{}
	created := serve(t, svc, http.MethodPost, "/studio/projects", `{"kind":"image","name":"Post","document":{"schema":"studio.image"}}`, nil)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"schema":"studio.image"`) {
		t.Fatalf("status %d body %s", created.Code, created.Body)
	}
	listed := serve(t, svc, http.MethodGet, "/studio/projects?kind=video", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"total":1`) {
		t.Fatalf("status %d body %s", listed.Code, listed.Body)
	}
}

func TestCapabilitiesAreReportedForTheSessionOfTheCaller(t *testing.T) {
	capabilities := &fakeCapabilities{}
	body := `{"kind":"video","capabilities":{"backend":"webgl","gpuRenderer":"ANGLE","webgpu":true,"decode":true,"encodeVideo":true,"encodeAudio":false,"pixelRatio":2,"cores":8,"memoryGb":8},"usage":{"frames":120,"stalls":2,"browserExports":1,"exportFailures":{"no_encoder":1}}}`
	rec := serveWith(t, &fakeService{}, capabilities, http.MethodPut, "/studio/capabilities/6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", body, map[string]string{"User-Agent": "Edge"})
	got := capabilities.got
	if rec.Code != http.StatusNoContent || got.SessionID != "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b" || got.WorkspaceID != "ws-1" || got.UserID != "u-1" || got.UserAgent != "Edge" {
		t.Fatalf("status %d report %+v", rec.Code, got)
	}
	if got.Kind != studio.KindVideo || got.Capabilities.Backend != studio.BackendWebGL || !got.Capabilities.WebGPU || got.Capabilities.MemoryGB != 8 || got.Usage.Stalls != 2 || got.Usage.ExportFailures[studio.FailureNoEncoder] != 1 {
		t.Fatalf("report %+v", got)
	}
}

func TestCapabilityReportsAnswerInvalidAndTakenSessions(t *testing.T) {
	invalid := &fakeCapabilities{err: &studio.ValidationError{Issues: []studio.FieldIssue{{Field: studio.FieldBackend, Code: studio.CodeUnknown}}}}
	if rec := serveWith(t, &fakeService{}, invalid, http.MethodPut, "/studio/capabilities/s-1", `{}`, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status %d", rec.Code)
	}
	taken := &fakeCapabilities{err: studio.ErrSessionTaken}
	rec := serveWith(t, &fakeService{}, taken, http.MethodPut, "/studio/capabilities/s-1", `{}`, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "session_taken") {
		t.Fatalf("taken status %d body %s", rec.Code, rec.Body)
	}
}

func TestAnExportedVideoIsStreamedToTheServiceAndAnsweredWithItsMedia(t *testing.T) {
	exports := &fakeExports{}
	body, contentType := exportForm(t, "video", []byte("mp4 bytes"))
	rec := serveAll(t, &fakeService{}, &fakeCapabilities{}, exports, http.MethodPost, "/studio/projects/p-1/exports", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusCreated || string(exports.received) != "mp4 bytes" || !strings.Contains(rec.Body.String(), `"mediaId":"e-1"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAnExportWithoutTheVideoPartIsRefused(t *testing.T) {
	body, contentType := exportForm(t, "other", []byte("mp4 bytes"))
	rec := serveAll(t, &fakeService{}, &fakeCapabilities{}, &fakeExports{}, http.MethodPost, "/studio/projects/p-1/exports", body, map[string]string{"Content-Type": contentType})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "invalid_export") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	plain := serveAll(t, &fakeService{}, &fakeCapabilities{}, &fakeExports{}, http.MethodPost, "/studio/projects/p-1/exports", "raw", map[string]string{"Content-Type": "video/mp4"})
	if plain.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a body that is not multipart: status %d", plain.Code)
	}
}

func TestExportsAnswerWhatWentWrong(t *testing.T) {
	cases := map[error][2]string{
		studio.ErrExportTooLarge: {"422", "export_too_large"},
		studio.ErrInvalidExport:  {"422", "invalid_export"},
		studio.ErrNotVideo:       {"422", "not_video"},
	}
	for err, want := range cases {
		body, contentType := exportForm(t, "video", []byte("mp4 bytes"))
		rec := serveAll(t, &fakeService{}, &fakeCapabilities{}, &fakeExports{err: err}, http.MethodPost, "/studio/projects/p-1/exports", body, map[string]string{"Content-Type": contentType})
		if strconv.Itoa(rec.Code) != want[0] || !strings.Contains(rec.Body.String(), want[1]) {
			t.Fatalf("%v: status %d body %s", err, rec.Code, rec.Body)
		}
	}
}
