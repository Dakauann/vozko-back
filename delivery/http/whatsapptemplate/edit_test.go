package whatsapptemplate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type fakeEditor struct {
	actor buttonaction_usecase.Actor
	in    buttonaction_usecase.EditTemplateRequest
	err   error
}

func (f *fakeEditor) EditTemplate(_ context.Context, a buttonaction_usecase.Actor, in buttonaction_usecase.EditTemplateRequest) (*template.Template, error) {
	f.actor, f.in = a, in
	if f.err != nil {
		return nil, f.err
	}
	return &template.Template{ID: in.TemplateID, Status: template.TemplateStatusPending, Category: template.TemplateCategoryMarketing}, nil
}

func serveEditWith(t *testing.T, h *WhatsAppTemplateHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serveEditAs(t, h, body, true)
}

func serveEdit(t *testing.T, editor ExitGuardedEditor, body string, withClaims bool) *httptest.ResponseRecorder {
	t.Helper()
	h := &WhatsAppTemplateHandler{}
	h.SetTemplateEditor(editor)
	return serveEditAs(t, h, body, withClaims)
}

func serveEditAs(t *testing.T, h *WhatsAppTemplateHandler, body string, withClaims bool) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/whatsapp/templates/{id}/content", h.EditForWorkspace).Methods(http.MethodPut)
	req := httptest.NewRequest(http.MethodPut, "/whatsapp/templates/tpl-1/content", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	if withClaims {
		ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func errorCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Code
}

func TestAnEditCarriesTheWorkspaceTheUserAndTheContent(t *testing.T) {
	editor := &fakeEditor{}
	body := `{"category":"MARKETING","components":[{"type":"BODY","text":"Oi"},{"type":"BUTTONS","buttons":[{"type":"QUICK_REPLY","text":"Parar"}]}],"headerMediaUrl":"https://files.test/a.jpg","buttonActions":[{"buttonIndex":0,"buttonText":"Parar","kind":"opt_out"}],"exitConfirmation":"Pronto."}`

	rec := serveEdit(t, editor, body, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	in := editor.in
	if editor.actor.WorkspaceID != "ws-1" || editor.actor.UserID != "user-1" || in.TemplateID != "tpl-1" || in.Edit.Category != template.TemplateCategoryMarketing ||
		len(in.Edit.Components) != 2 || in.Edit.HeaderMediaURL == nil {
		t.Fatalf("actor = %+v, in = %+v", editor.actor, in)
	}
	if len(in.Actions) != 1 || in.Actions[0].Kind != "opt_out" || in.Actions[0].ButtonText != "Parar" || in.ExitConfirmation != "Pronto." {
		t.Fatalf("exit setup = %+v, %q", in.Actions, in.ExitConfirmation)
	}
}

func TestAnEditWithoutAUserIsUnauthorized(t *testing.T) {
	if rec := serveEdit(t, &fakeEditor{}, `{"components":[]}`, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestEditRefusalsAreCodedWithTheirStatus(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{template.ErrTemplateAccessDenied, http.StatusForbidden, "template_access_denied"},
		{template.ErrEditNotOwner, http.StatusForbidden, "template_edit_not_owner"},
		{template.ErrTemplateInUse, http.StatusConflict, "template_in_use"},
		{template.ErrTemplateNotEditable, http.StatusConflict, "template_not_editable"},
		{template.ErrEditCategoryLocked, http.StatusConflict, "template_edit_category_locked"},
		{template.ErrEditUnsupported, http.StatusConflict, "template_edit_unsupported"},
		{&template.EditWindowError{NextAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}, http.StatusTooManyRequests, "template_edit_window"},
		{template.ErrBodyNeedsExample, http.StatusBadRequest, "template_body_needs_example"},
		{buttonaction.ErrMarketingNeedsExit, http.StatusUnprocessableEntity, "marketing_template_needs_exit"},
	}
	for _, tc := range cases {
		rec := serveEdit(t, &fakeEditor{err: tc.err}, `{"components":[{"type":"BODY","text":"Oi"}]}`, true)
		if rec.Code != tc.status || errorCodeOf(t, rec) != tc.code {
			t.Errorf("%v: status %d code %q, want %d %q", tc.err, rec.Code, errorCodeOf(t, rec), tc.status, tc.code)
		}
	}
}

func TestTheEditWindowSaysWhenTheNextEditIsAllowed(t *testing.T) {
	rec := serveEdit(t, &fakeEditor{err: &template.EditWindowError{NextAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}}, `{"components":[]}`, true)
	if !strings.Contains(rec.Body.String(), `"nextEditAt":"2026-10-18T12:00:00Z"`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestAnUnwiredEditorFailsClosed(t *testing.T) {
	h := &WhatsAppTemplateHandler{}
	router := mux.NewRouter()
	router.HandleFunc("/whatsapp/templates/{id}/content", h.EditForWorkspace).Methods(http.MethodPut)
	req := httptest.NewRequest(http.MethodPut, "/whatsapp/templates/tpl-1/content", strings.NewReader(`{"components":[]}`))
	ctx := context.WithValue(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1"), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
