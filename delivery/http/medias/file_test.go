package medias

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	mediadomain "vozko/domain/media"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type fakeReader struct {
	content   *mediadomain.Content
	err       error
	workspace string
	mediaID   string
}

func (r *fakeReader) Read(_ context.Context, workspaceID, mediaID string) (*mediadomain.Content, error) {
	r.workspace, r.mediaID = workspaceID, mediaID
	return r.content, r.err
}

type passLimiter struct{}

func (passLimiter) Validate(next http.Handler) http.Handler { return next }

func serveFile(t *testing.T, reader *fakeReader, grant func(workspace_domain.Resource, workspace_domain.Action) bool) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	ac := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !grant(resource, action) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	RegisterRoutes(router, NewMediasHandler(nil, nil, nil, reader), ac, passLimiter{})
	req := httptest.NewRequest(http.MethodGet, "/medias/m-1/file", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func readOnly(resource workspace_domain.Resource, action workspace_domain.Action) bool {
	return resource == workspace_domain.ResourceMedia && action == workspace_domain.ActionRead
}

func TestAMediaFileIsServedAsAnAttachmentFromTheWorkspace(t *testing.T) {
	reader := &fakeReader{content: &mediadomain.Content{
		Media:       &mediadomain.Media{ID: "m-1"},
		Name:        "Imagem gerada com IA",
		ContentType: "image/jpeg",
		Data:        []byte("jpeg-bytes"),
	}}
	rec := serveFile(t, reader, readOnly)
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg-bytes" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
	if reader.workspace != "ws-1" || reader.mediaID != "m-1" {
		t.Fatalf("read %s/%s", reader.workspace, reader.mediaID)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Content-Disposition") != `attachment; filename="m-1.jpg"` || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %v", rec.Header())
	}
}

func TestAnUnlabelledFileIsSniffed(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	reader := &fakeReader{content: &mediadomain.Content{Media: &mediadomain.Media{ID: "m-1"}, Data: png}}
	rec := serveFile(t, reader, readOnly)
	if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Content-Disposition") != `attachment; filename="m-1.png"` {
		t.Fatalf("headers %v", rec.Header())
	}
}

func TestDownloadingNeedsMediaRead(t *testing.T) {
	reader := &fakeReader{content: &mediadomain.Content{Media: &mediadomain.Media{ID: "m-1"}, Data: []byte("x")}}
	rec := serveFile(t, reader, func(workspace_domain.Resource, workspace_domain.Action) bool { return false })
	if rec.Code != http.StatusForbidden || reader.mediaID != "" {
		t.Fatalf("status %d read %q", rec.Code, reader.mediaID)
	}
}

func TestMediaFileErrorsMapToStableStatuses(t *testing.T) {
	cases := map[error]int{
		mediadomain.ErrMediaNotFound: http.StatusNotFound,
		mediadomain.ErrMediaTooLarge: http.StatusRequestEntityTooLarge,
		errors.New("r2 down"):        http.StatusInternalServerError,
	}
	for err, status := range cases {
		rec := serveFile(t, &fakeReader{err: err}, readOnly)
		if rec.Code != status {
			t.Fatalf("%v: status %d", err, rec.Code)
		}
	}
}
