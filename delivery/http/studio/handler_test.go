package studiohttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/mediagen"
	"vozko/domain/studio"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type fakeService struct {
	project   *studio.Project
	saveErr   error
	exportErr error
	version   int64
	rasters   map[string]string
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

func (f *fakeService) Export(_ context.Context, _, _, _ string, version int64, rasters map[string]string) (*mediagen.Job, error) {
	f.version, f.rasters = version, rasters
	if f.exportErr != nil {
		return nil, f.exportErr
	}
	return &mediagen.Job{ID: "job-1", Kind: mediagen.KindVideo, Status: mediagen.StatusQueued}, nil
}

func serve(t *testing.T, svc *fakeService, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	allow := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc { return next }
	RegisterProtectedRoutes(router, NewHandler(svc), allow)
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
	var body ConflictResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusConflict || body.Code != "version_conflict" || body.Current.Version != 4 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestExportSendsTheVersionAndRasters(t *testing.T) {
	svc := &fakeService{project: current()}
	rec := serve(t, svc, http.MethodPost, "/studio/projects/p-1/export", `{"version":4,"rasters":{"o1":"r-1"}}`, nil)
	if rec.Code != http.StatusAccepted || svc.version != 4 || svc.rasters["o1"] != "r-1" || !strings.Contains(rec.Body.String(), `"job-1"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	missing := serve(t, &fakeService{project: current(), exportErr: studio.ErrNotRasterized}, http.MethodPost, "/studio/projects/p-1/export", `{"version":4}`, nil)
	if missing.Code != http.StatusUnprocessableEntity || !strings.Contains(missing.Body.String(), "not_rasterized") {
		t.Fatalf("status %d body %s", missing.Code, missing.Body)
	}
	capped := serve(t, &fakeService{project: current(), exportErr: mediagen.ErrTooManyActive}, http.MethodPost, "/studio/projects/p-1/export", `{"version":4}`, nil)
	if capped.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", capped.Code)
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
