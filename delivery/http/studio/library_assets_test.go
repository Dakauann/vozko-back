package studiohttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/media"
	"vozko/domain/studio/library"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	media_usecase "vozko/usecases/media"
)

type fakeLibraryAssets struct {
	err                     error
	workspace, item, locale string
	body                    string
}

func (f *fakeLibraryAssets) Import(_ context.Context, workspaceID, itemID, locale string, file io.Reader) (media.Media, error) {
	raw, _ := io.ReadAll(file)
	f.workspace, f.item, f.locale, f.body = workspaceID, itemID, locale, string(raw)
	if f.err != nil {
		return media.Media{}, f.err
	}
	return media.Media{ID: "m-1", URL: "https://cdn.test/m-1.webp"}, nil
}

func serveLibraryImport(t *testing.T, assets LibraryAssetImporter, path string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	granted := workspace_domain.Action("")
	allow := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if resource == workspace_domain.ResourceMedia && strings.Contains(r.URL.Path, "/library/") {
				granted = action
			}
			next(w, r)
		}
	}
	handler := NewHandler(&fakeService{}, &fakeCapabilities{}, &fakeExports{}, &fakePresets{})
	if assets != nil {
		handler = handler.WithLibraryAssets(assets)
	}
	RegisterProtectedRoutes(router, handler, allow)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("webp bytes"))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	if granted != workspace_domain.ActionCreate {
		t.Fatalf("the import asked for %q", granted)
	}
	return rec
}

func TestALibraryPictureImportAnswersTheWorkspaceMedia(t *testing.T) {
	assets := &fakeLibraryAssets{}
	rec := serveLibraryImport(t, assets, "/studio/library/items/3d-icon-rocket/import?locale=en")
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d %s", rec.Code, rec.Body.String())
	}
	var body LibraryAssetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.MediaID != "m-1" || body.MediaURL == "" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if assets.workspace != "ws-1" || assets.item != "3d-icon-rocket" || assets.locale != "en" || assets.body != "webp bytes" {
		t.Fatalf("assets = %+v", assets)
	}
}

func TestLibraryPictureImportRefusals(t *testing.T) {
	cases := map[string]struct {
		err  error
		code int
	}{
		"unknown":  {library.ErrUnknownAsset, http.StatusNotFound},
		"mismatch": {library.ErrAssetMismatch, http.StatusUnprocessableEntity},
		"limit":    {media_usecase.ErrUploadLimit, http.StatusConflict},
		"broken":   {errors.New("db down"), http.StatusInternalServerError},
	}
	for name, tc := range cases {
		if rec := serveLibraryImport(t, &fakeLibraryAssets{err: tc.err}, "/studio/library/items/x/import"); rec.Code != tc.code {
			t.Errorf("%s: code = %d", name, rec.Code)
		}
	}
	if rec := serveLibraryImport(t, nil, "/studio/library/items/x/import"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without the service: code = %d", rec.Code)
	}
}
