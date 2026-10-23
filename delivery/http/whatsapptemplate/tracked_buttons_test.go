package whatsapptemplate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/choicestats"
	"vozko/domain/whatsapp/template"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type fakeTracking struct {
	calls   int
	indexes []int
	err     error
}

func (f *fakeTracking) Apply(_ context.Context, workspaceID, userID string, components []template.TemplateComponent, tracked []int) ([]template.TemplateComponent, error) {
	f.calls++
	f.indexes = tracked
	if f.err != nil {
		return nil, f.err
	}
	out := append([]template.TemplateComponent(nil), components...)
	out[1].Buttons = []template.TemplateButton{{Type: "URL", Text: "Loja", URL: choicestats.TrackedButtonURL("https://vz.co/r", "Ab3x")}}
	return out, nil
}

const trackedEditBody = `{"components":[{"type":"BODY","text":"Oi"},{"type":"BUTTONS","buttons":[{"type":"URL","text":"Loja","url":"https://loja.com"}]}],"trackedUrlButtons":[0]}`

func serveTrackedEdit(t *testing.T, tracking *fakeTracking, editor *fakeEditor, body string) int {
	t.Helper()
	h := &WhatsAppTemplateHandler{}
	if tracking != nil {
		h.SetClickTracking(tracking, "https://vz.co/r")
	}
	h.SetTemplateEditor(editor)
	return serveEditWith(t, h, body).Code
}

func TestAnEditWithTrackedButtonsSubmitsTheTrackedLinks(t *testing.T) {
	tracking, editor := &fakeTracking{}, &fakeEditor{}
	if code := serveTrackedEdit(t, tracking, editor, trackedEditBody); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if tracking.calls != 1 || len(tracking.indexes) != 1 || tracking.indexes[0] != 0 {
		t.Fatalf("tracking = %+v", tracking)
	}
	if got := editor.in.Edit.Components[1].Buttons[0].URL; got != "https://vz.co/r/Ab3x/{{1}}" {
		t.Fatalf("submitted url = %q", got)
	}
}

func TestAnEditWithoutTheFieldLeavesTrackingAlone(t *testing.T) {
	tracking := &fakeTracking{}
	if code := serveTrackedEdit(t, tracking, &fakeEditor{}, `{"components":[{"type":"BODY","text":"Oi"}]}`); code != http.StatusOK || tracking.calls != 0 {
		t.Fatalf("status = %d, calls = %d", code, tracking.calls)
	}
}

func TestTrackingRefusalsAreCoded(t *testing.T) {
	h := &WhatsAppTemplateHandler{}
	h.SetClickTracking(&fakeTracking{err: template.ErrTrackedButtonNotURL}, "https://vz.co/r")
	h.SetTemplateEditor(&fakeEditor{})
	rec := serveEditWith(t, h, trackedEditBody)
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != template.CodeTrackedButtonNotURL {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}

	unwired := &WhatsAppTemplateHandler{}
	unwired.SetTemplateEditor(&fakeEditor{})
	rec = serveEditWith(t, unwired, trackedEditBody)
	if rec.Code != http.StatusServiceUnavailable || errorCodeOf(t, rec) != codeTrackingUnavailable {
		t.Fatalf("an unwired tracking = %d %s", rec.Code, rec.Body)
	}
}

func TestTheTemplateReadAlwaysCarriesItsTrackedButtons(t *testing.T) {
	tracked := &template.Template{ID: "t", Components: []template.TemplateComponent{{Type: "BUTTONS", Buttons: []template.TemplateButton{
		{Type: "QUICK_REPLY", Text: "Quero"}, {Type: "URL", Text: "Loja", URL: choicestats.TrackedButtonURL("https://vz.co/r", "Ab3x")},
	}}}}
	for _, tc := range []struct {
		tmpl *template.Template
		want string
	}{{tracked, `[1]`}, {&template.Template{ID: "p"}, `[]`}} {
		raw, err := json.Marshal(toTemplateResponse(tc.tmpl, "https://vz.co/r"))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		if string(body["trackedUrlButtons"]) != tc.want {
			t.Fatalf("trackedUrlButtons = %s, want %s", body["trackedUrlButtons"], tc.want)
		}
	}
}

type fakeCreator struct {
	in buttonaction_usecase.CreateTemplateRequest
}

func (f *fakeCreator) CreateTemplate(_ context.Context, _ buttonaction_usecase.Actor, in buttonaction_usecase.CreateTemplateRequest) (*template.CreateTemplateOutput, error) {
	f.in = in
	return &template.CreateTemplateOutput{}, nil
}

func TestACreateWithTrackedButtonsSubmitsTheTrackedLinks(t *testing.T) {
	tracking, creator := &fakeTracking{}, &fakeCreator{}
	h := &WhatsAppTemplateHandler{}
	h.SetClickTracking(tracking, "https://vz.co/r")
	h.SetExitGuardedCreator(creator)
	body := `{"businessPhoneId":"bp-1","name":"oferta","language":"pt_BR","category":"MARKETING","components":[{"type":"BODY","text":"Oi"},{"type":"BUTTONS","buttons":[{"type":"URL","text":"Loja","url":"https://loja.com"}]}],"trackedUrlButtons":[0]}`
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/templates", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	rec := httptest.NewRecorder()
	h.CreateForWorkspace(rec, req.WithContext(ctx))
	if tracking.calls != 1 || creator.in.Template.Components[1].Buttons[0].URL != "https://vz.co/r/Ab3x/{{1}}" {
		t.Fatalf("status = %d body = %s tracking = %+v", rec.Code, rec.Body, tracking)
	}
}

func TestATrackingRefusalNamesTheButtonSoTheEditorShowsItInline(t *testing.T) {
	h := &WhatsAppTemplateHandler{}
	h.SetClickTracking(&fakeTracking{err: template.TrackedButtonRefusal(2, template.ErrTrackedButtonDynamic)}, "https://vz.co/r")
	h.SetTemplateEditor(&fakeEditor{})
	rec := serveEditWith(t, h, trackedEditBody)
	var body struct {
		Expected map[string]string `json:"expected"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != template.CodeTrackedButtonDynamic || body.Expected["buttonIndex"] != "2" {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
}
