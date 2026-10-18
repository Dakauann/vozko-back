package buttonactionhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type fakeService struct {
	saveErr      error
	savedVersion int64
	savedChange  buttonaction_usecase.TemplateChange
	routesErr    error
	draft        buttonaction_usecase.DraftTemplate
	draftErr     error
}

func sampleTemplate() *template.Template {
	return &template.Template{ID: "tmpl-1", Name: "promo", Language: "pt_BR", Components: []template.TemplateComponent{
		{Type: "BODY", Text: "Oi"},
		{Type: "BUTTONS", Buttons: []template.TemplateButton{{Type: "QUICK_REPLY", Text: "SAIR"}}},
	}}
}

func sampleView() *buttonaction_usecase.TemplateView {
	tmpl := sampleTemplate()
	action, _ := buttonaction.NewButtonAction(tmpl, buttonaction.Input{ButtonIndex: 0, Kind: buttonaction.KindOptOut})
	settings := buttonaction.TemplateSettings{Template: buttonaction.RefOf(tmpl), Actions: []buttonaction.ButtonAction{action}, Version: 3}.Normalized()
	return &buttonaction_usecase.TemplateView{TemplateID: tmpl.ID, Template: tmpl, Settings: settings, Buttons: buttonaction.RoutesOf(tmpl, settings, buttonaction.CampaignRoutes{}),
		AllowedKinds: []buttonaction.Kind{buttonaction.KindOptOut, buttonaction.KindClassify}}
}

func (f *fakeService) TemplateActions(context.Context, buttonaction_usecase.Actor, string) (*buttonaction_usecase.TemplateView, error) {
	return sampleView(), nil
}

func (f *fakeService) SaveTemplateActions(_ context.Context, _ buttonaction_usecase.Actor, _ string, version int64, change buttonaction_usecase.TemplateChange) (*buttonaction_usecase.TemplateView, error) {
	f.savedVersion, f.savedChange = version, change
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	return sampleView(), nil
}

func (f *fakeService) ExitVerdict(context.Context, buttonaction_usecase.Actor, string, string) (*buttonaction_usecase.VerdictView, error) {
	tmpl := sampleTemplate()
	return &buttonaction_usecase.VerdictView{Template: tmpl, Verdict: buttonaction.ExitVerdict(tmpl, buttonaction.TemplateSettings{}, buttonaction.CampaignRoutes{}),
		Gates: buttonaction.ExitGates{CampaignAcknowledgementRequired: true}}, nil
}

func (f *fakeService) DraftExitVerdict(_ context.Context, _ buttonaction_usecase.Actor, draft buttonaction_usecase.DraftTemplate) (*buttonaction_usecase.DraftVerdictView, error) {
	f.draft = draft
	if f.draftErr != nil {
		return nil, f.draftErr
	}
	tmpl := sampleTemplate()
	return &buttonaction_usecase.DraftVerdictView{Template: tmpl, Verdict: buttonaction.ExitVerdict(tmpl, buttonaction.TemplateSettings{}, buttonaction.CampaignRoutes{}),
		Gates: buttonaction.ExitGates{TemplateExitRequired: true, CampaignAcknowledgementRequired: true}, AllowedKinds: []buttonaction.Kind{buttonaction.KindOptOut}}, nil
}

func (f *fakeService) CampaignRoutes(context.Context, buttonaction_usecase.Actor, string) (*buttonaction_usecase.CampaignView, error) {
	return nil, buttonaction.ErrCampaignNotFound
}

func (f *fakeService) SaveCampaignRoutes(context.Context, buttonaction_usecase.Actor, string, int64, []buttonaction.Input) (*buttonaction_usecase.CampaignView, error) {
	return nil, f.routesErr
}

func allow(_ workspace_domain.Resource, _ workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return h
}

func serve(t *testing.T, svc Service, method, path, ifMatch, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, NewHandler(svc), allow)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != "" {
		return body.Code
	}
	return body.Error.Code
}

func TestGetTemplateActionsShowsEachButtonWithItsAction(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodGet, "/whatsapp/templates/tmpl-1/button-actions", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"buttons":[{"index":0,"text":"SAIR","action":{`, `"kind":"opt_out"`, `"source":"template"`, `"version":3`, `"exitConfirmation":""`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body misses %s: %s", want, rec.Body)
		}
	}
	for _, gone := range []string{`"exitWords"`, `"exitRiskAcknowledgement"`} {
		if strings.Contains(rec.Body.String(), gone) {
			t.Errorf("body still carries %s: %s", gone, rec.Body)
		}
	}
}

func TestSaveTemplateActionsNeedsTheVersion(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", "", `{"actions":[]}`)
	if rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSaveTemplateActionsPassesTheBodyAndVersion(t *testing.T) {
	svc := &fakeService{}
	body := `{"actions":[{"buttonIndex":0,"kind":"opt_out","params":{},"confirmation":"Ok"}],"exitConfirmation":"Pronto"}`
	rec := serve(t, svc, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", `"3"`, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.savedVersion != 3 || len(svc.savedChange.Actions) != 1 || svc.savedChange.Actions[0].Kind != buttonaction.KindOptOut || svc.savedChange.ExitConfirmation != "Pronto" {
		t.Fatalf("saved = %d %+v", svc.savedVersion, svc.savedChange)
	}
}

func TestSaveTemplateActionsRefusesUnknownKeys(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", "3", `{"actions":[],"stale":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	words := serve(t, &fakeService{}, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", "3", `{"actions":[],"exitWords":["/sair"]}`)
	if words.Code != http.StatusBadRequest {
		t.Fatalf("template exit words are no longer stored, status = %d", words.Code)
	}
}

func TestSaveTemplateActionsConflictCarriesTheCurrentState(t *testing.T) {
	rec := serve(t, &fakeService{saveErr: buttonaction.ErrVersionConflict}, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", "2", `{"actions":[]}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"version_conflict"`) || !strings.Contains(rec.Body.String(), `"version":3`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestRefusalsCarryTheirCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{buttonaction.ErrForbidden, http.StatusForbidden, "button_action_forbidden"},
		{buttonaction.ErrExitLocked, http.StatusUnprocessableEntity, "button_action_exit_locked"},
		{buttonaction.ErrParamsInvalid, http.StatusBadRequest, "button_action_params_invalid"},
		{buttonaction.ErrExitRiskNotAcknowledged, http.StatusUnprocessableEntity, "exit_risk_not_acknowledged"},
		{buttonaction.ErrMarketingNeedsExit, http.StatusUnprocessableEntity, "marketing_template_needs_exit"},
	}
	for _, tc := range cases {
		rec := serve(t, &fakeService{routesErr: tc.err}, http.MethodPut, "/whatsapp/campaigns/camp-1/button-routes", "2", `{"routes":[]}`)
		if rec.Code != tc.status || codeOf(t, rec) != tc.code {
			t.Errorf("%v: status = %d, code = %q, body = %s", tc.err, rec.Code, codeOf(t, rec), rec.Body)
		}
	}
}

func TestExitVerdictLists(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodGet, "/whatsapp/templates/tmpl-1/exit-verdict?campaignId=camp-1", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"none"`) || !strings.Contains(rec.Body.String(), `"action":null`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), `"workspaceWords"`) {
		t.Fatalf("workspace words never take part in a template verdict: %s", rec.Body)
	}
}
