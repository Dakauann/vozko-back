package studiohttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/studio"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type fakePresets struct {
	err         error
	created     []string
	change      studio.PresetChange
	deleted     string
	workspace   string
	createdBy   string
	description string
}

func (f *fakePresets) List(_ context.Context, ws string) ([]studio.AnimationPreset, error) {
	f.workspace = ws
	return []studio.AnimationPreset{
		{ID: "builtin.riseIn", Name: "Subir e surgir", BuiltIn: true, Names: map[string]string{"pt": "Subir e surgir", "en": "Rise in"}, Animation: json.RawMessage(`{"scope":"visual"}`)},
		{ID: "p-1", Name: "Giro", Animation: json.RawMessage(`{"scope":"text"}`), CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
	}, f.err
}

func (f *fakePresets) Create(_ context.Context, ws, user, name, description string, animation json.RawMessage) (*studio.AnimationPreset, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.workspace, f.createdBy, f.description = ws, user, description
	f.created = append(f.created, name)
	return &studio.AnimationPreset{ID: "p-2", WorkspaceID: ws, Name: name, Description: description, Animation: animation}, nil
}

func (f *fakePresets) Update(_ context.Context, _, id string, change studio.PresetChange) (*studio.AnimationPreset, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.change = change
	return &studio.AnimationPreset{ID: id, Name: *change.Name, Animation: json.RawMessage(`{}`)}, nil
}

func (f *fakePresets) Delete(_ context.Context, _, id string) error {
	f.deleted = id
	return f.err
}

func servePresets(t *testing.T, presets *fakePresets, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	granted := map[string]workspace_domain.Action{}
	allow := func(resource workspace_domain.Resource, action workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if resource != workspace_domain.ResourceMedia {
				t.Fatalf("resource %s", resource)
			}
			granted[r.Method] = action
			next(w, r)
		}
	}
	RegisterProtectedRoutes(router, NewHandler(&fakeService{}, &fakeCapabilities{}, &fakeExports{}, presets), allow)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	want := map[string]workspace_domain.Action{
		http.MethodGet: workspace_domain.ActionRead, http.MethodPost: workspace_domain.ActionCreate,
		http.MethodPatch: workspace_domain.ActionCreate, http.MethodDelete: workspace_domain.ActionDelete,
	}
	if action, ok := granted[method]; ok && action != want[method] {
		t.Fatalf("%s asked for %s", method, action)
	}
	return rec
}

func TestPresetsAreListedWithBuiltinsMarked(t *testing.T) {
	presets := &fakePresets{}
	rec := servePresets(t, presets, http.MethodGet, "/studio/animation-presets", "")
	var body PresetListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || len(body.Items) != 2 || presets.workspace != "ws-1" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	builtin, saved := body.Items[0], body.Items[1]
	if !builtin.BuiltIn || builtin.Names["en"] != "Rise in" || builtin.CreatedAt != nil || string(builtin.Animation) != `{"scope":"visual"}` {
		t.Fatalf("builtin %+v", builtin)
	}
	if saved.BuiltIn || saved.CreatedAt == nil || saved.UpdatedAt == nil || saved.Names != nil {
		t.Fatalf("saved %+v", saved)
	}
	if strings.Contains(rec.Body.String(), `"descriptions"`) {
		t.Fatalf("empty maps leak: %s", rec.Body)
	}
}

func TestAPresetIsCreatedForTheRequester(t *testing.T) {
	presets := &fakePresets{}
	rec := servePresets(t, presets, http.MethodPost, "/studio/animation-presets", `{"name":"Giro","description":"Uma volta","animation":{"scope":"visual","durationMs":1000}}`)
	if rec.Code != http.StatusCreated || presets.createdBy != "u-1" || presets.description != "Uma volta" || !strings.Contains(rec.Body.String(), `"durationMs":1000`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if rec := servePresets(t, presets, http.MethodPost, "/studio/animation-presets", `{"name":"Giro","animation":{},"extra":true}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown envelope field status %d", rec.Code)
	}
	huge := `{"name":"x","animation":"` + strings.Repeat("a", studio.MaxPresetAnimationBytes+presetEnvelopeBytes) + `"}`
	if rec := servePresets(t, presets, http.MethodPost, "/studio/animation-presets", huge); rec.Code != http.StatusBadRequest || len(presets.created) != 1 {
		t.Fatalf("huge status %d created %d", rec.Code, len(presets.created))
	}
}

func TestPresetValidationAnswersTheFieldCodes(t *testing.T) {
	_, invalid := studio.NewAnimationPreset("ws-1", "u-1", "x", "", json.RawMessage(`{"scope":"audio","durationMs":1000}`))
	presets := &fakePresets{err: invalid}
	rec := servePresets(t, presets, http.MethodPost, "/studio/animation-presets", `{"name":"x","animation":{"scope":"audio"}}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"animation":"unknown"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAPresetIsRenamedAndDeleted(t *testing.T) {
	presets := &fakePresets{}
	rec := servePresets(t, presets, http.MethodPatch, "/studio/animation-presets/p-1", `{"name":"Novo"}`)
	if rec.Code != http.StatusOK || presets.change.Name == nil || *presets.change.Name != "Novo" || presets.change.Description != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if rec := servePresets(t, presets, http.MethodDelete, "/studio/animation-presets/p-1", ""); rec.Code != http.StatusNoContent || presets.deleted != "p-1" {
		t.Fatalf("delete status %d", rec.Code)
	}
}

func TestPresetErrorsMapToTheirStatus(t *testing.T) {
	for err, want := range map[error][2]string{
		studio.ErrPresetNotFound: {"404", "Predefinição não encontrada"},
		studio.ErrPresetReadOnly: {"409", "read_only"},
	} {
		presets := &fakePresets{err: err}
		patch := servePresets(t, presets, http.MethodPatch, "/studio/animation-presets/builtin.riseIn", `{"name":"x"}`)
		del := servePresets(t, presets, http.MethodDelete, "/studio/animation-presets/builtin.riseIn", "")
		for _, rec := range []*httptest.ResponseRecorder{patch, del} {
			if strconv.Itoa(rec.Code) != want[0] || !strings.Contains(rec.Body.String(), want[1]) {
				t.Fatalf("%v: status %d body %s", err, rec.Code, rec.Body)
			}
		}
	}
}
