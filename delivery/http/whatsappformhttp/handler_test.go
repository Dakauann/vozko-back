package whatsappformhttp

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
	"vozko/domain/whatsappform"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type fakeService struct {
	Service
	err        error
	gotQuery   whatsappform.ResponseQuery
	gotDraft   whatsappform.Document
	gotBase    string
	gotSend    whatsappform_usecase.SendInput
	gotPeriod  whatsappform_usecase.Period
	gotSendQ   whatsappform_usecase.SendableQuery
	gotCreated whatsappform_usecase.CreateInput
	gotChosen  []string
}

func (f *fakeService) Create(_ context.Context, _ Actor, in whatsappform_usecase.CreateInput) (whatsappform_usecase.FormDetail, error) {
	f.gotCreated = in
	return whatsappform_usecase.FormDetail{Form: whatsappform.Form{ID: "form-1", Name: in.Name}}, f.err
}

func (f *fakeService) SaveDraft(_ context.Context, _ Actor, _ string, doc whatsappform.Document, base string) (whatsappform.Version, error) {
	f.gotDraft, f.gotBase = doc, base
	return whatsappform.Version{ID: "v-2"}, f.err
}

func (f *fakeService) ListResponses(_ context.Context, _ Actor, q whatsappform.ResponseQuery) (whatsappform.Page[whatsappform_usecase.ResponseRow], error) {
	f.gotQuery = q
	return whatsappform.Page[whatsappform_usecase.ResponseRow]{Next: "cursor-2"}, f.err
}

func (f *fakeService) SendInConversation(_ context.Context, _ Actor, in whatsappform_usecase.SendInput) (whatsappform.Send, error) {
	f.gotSend = in
	return whatsappform.Send{ID: "send-1"}, f.err
}

func (f *fakeService) Stats(_ context.Context, _ Actor, _ string, p whatsappform_usecase.Period) (whatsappform.Stats, error) {
	f.gotPeriod = p
	return whatsappform.Stats{}, f.err
}

func (f *fakeService) Sendable(_ context.Context, _ Actor, q whatsappform_usecase.SendableQuery) ([]whatsappform_usecase.SendableForm, error) {
	f.gotSendQ = q
	return nil, f.err
}

func (f *fakeService) Publish(_ context.Context, _ Actor, _ string, metaWABAIDs []string) (whatsappform_usecase.PublishResult, error) {
	f.gotChosen = metaWABAIDs
	return whatsappform_usecase.PublishResult{}, f.err
}

func (f *fakeService) PublishTargets(_ context.Context, _ Actor) ([]whatsappform_usecase.PublishTarget, error) {
	return []whatsappform_usecase.PublishTarget{{MetaWABAID: "waba-a", Supported: true}}, f.err
}

func serve(t *testing.T, svc *fakeService, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	RegisterRoutes(router, NewHandler(svc), func(_ workspace_domain.Resource, _ workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return h
	})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	authed := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	req = req.WithContext(context.WithValue(authed, middleware.WorkspaceIDContextKey, "ws-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestErrorsMapToStatusesAndCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{whatsappform.ErrForbidden, http.StatusForbidden, "forbidden"},
		{whatsappform.ErrNotOwner, http.StatusForbidden, "whatsapp_form_not_owner"},
		{whatsappform.ErrFormNotFound, http.StatusNotFound, "whatsapp_form_not_found"},
		{whatsappform.ErrResponseNotFound, http.StatusNotFound, "whatsapp_form_response_not_found"},
		{whatsappform.ErrStaleDraft, http.StatusConflict, "whatsapp_form_stale_draft"},
		{whatsappform.ErrNameTaken, http.StatusConflict, "whatsapp_form_name_taken"},
		{whatsappform.ErrOutsideWindow, http.StatusConflict, "whatsapp_form_outside_window"},
		{whatsappform.ErrNoPublication, http.StatusConflict, "whatsapp_form_not_published"},
		{whatsappform.ErrPublicationNotSendable, http.StatusConflict, "whatsapp_form_not_sendable"},
		{whatsappform.ErrProviderUnsupported, http.StatusConflict, "whatsapp_form_provider_unsupported"},
		{whatsappform.ErrNoEligibleAccount, http.StatusConflict, "whatsapp_form_no_account"},
		{whatsappform.ErrNoAccountChosen, http.StatusBadRequest, "whatsapp_form_no_account_chosen"},
		{whatsappform.ErrFormArchived, http.StatusConflict, "whatsapp_form_archived"},
		{whatsappform.ErrNameRequired, http.StatusBadRequest, "whatsapp_form_name_required"},
		{whatsappform.ErrCopyInvalid, http.StatusBadRequest, "whatsapp_form_copy_invalid"},
		{&whatsappform.MetaRejection{Code: 139000, Message: "x"}, http.StatusBadGateway, "whatsapp_form_meta_rejected"},
		{errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, tc := range cases {
		rec := serve(t, &fakeService{err: tc.err}, http.MethodPost, "/whatsapp/forms", `{"name":"Cadastro"}`)
		if rec.Code != tc.status {
			t.Errorf("%v: status %d, want %d", tc.err, rec.Code, tc.status)
			continue
		}
		if tc.code != "" && decode(t, rec)["code"] != tc.code {
			t.Errorf("%v: body %s", tc.err, rec.Body.String())
		}
	}
}

func TestBuilderProblemsAre422WithTheProblemsInDetails(t *testing.T) {
	problems := whatsappform.Problems{{Screen: "s_a", Block: "b_x", Property: "label", Code: "required"}}
	rec := serve(t, &fakeService{err: problems}, http.MethodPost, "/whatsapp/forms/form-1x/publish", `{"metaWabaIds":["waba-a"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a form id that is not a uuid does not route, got %d", rec.Code)
	}
	rec = serve(t, &fakeService{err: problems}, http.MethodPost, "/whatsapp/forms/0f8c2d1e-0000-4000-8000-000000000001/publish", `{"metaWabaIds":["waba-a"]}`)
	body := decode(t, rec)
	details, _ := body["details"].(map[string]any)
	list, _ := details["problems"].([]any)
	if rec.Code != http.StatusUnprocessableEntity || body["code"] != "whatsapp_form_invalid" || len(list) != 1 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestCreateReturns201(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, http.MethodPost, "/whatsapp/forms", `{"name":"Cadastro","starter":"lead_capture"}`)
	if rec.Code != http.StatusCreated || svc.gotCreated.Starter != "lead_capture" {
		t.Fatalf("status %d input %+v", rec.Code, svc.gotCreated)
	}
	if rec := serve(t, svc, http.MethodPost, "/whatsapp/forms", `{"name":"x","unknown":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown fields are refused, got %d", rec.Code)
	}
}

func TestSaveDraftReadsTheDocumentAndItsBase(t *testing.T) {
	svc := &fakeService{}
	body := `{"baseVersionId":"v-1","document":{"schema":1,"categories":["OTHER"],"screens":[{"key":"s_a","title":"A","blocks":[],"route":{},"submitLabel":"Enviar"}]}}`
	rec := serve(t, svc, http.MethodPut, "/whatsapp/forms/0f8c2d1e-0000-4000-8000-000000000001/draft", body)
	if rec.Code != http.StatusOK || svc.gotBase != "v-1" || len(svc.gotDraft.Screens) != 1 || svc.gotDraft.Screens[0].Key != "s_a" {
		t.Fatalf("status %d base %q doc %+v", rec.Code, svc.gotBase, svc.gotDraft)
	}
}

func TestResponseListQueryParameters(t *testing.T) {
	svc := &fakeService{}
	path := "/whatsapp/forms/0f8c2d1e-0000-4000-8000-000000000001/responses?from=2026-10-01&to=2026-10-19&origin=inbox,campaign&phoneId=p1&versionId=v1&match=matched&search=ana&after=abc&limit=20&campaignId=c1"
	rec := serve(t, svc, http.MethodGet, path, "")
	q := svc.gotQuery
	if rec.Code != http.StatusOK || q.FormID != "0f8c2d1e-0000-4000-8000-000000000001" || q.Limit != 20 || q.After != "abc" || q.Search != "ana" ||
		q.Match != whatsappform.MatchFilterMatched || len(q.Origins) != 2 || q.PhoneIDs[0] != "p1" || q.VersionIDs[0] != "v1" || q.CampaignID != "c1" {
		t.Fatalf("status %d query %+v", rec.Code, q)
	}
	if q.From == nil || q.To == nil || !q.To.After(*q.From) || q.To.Hour() != 23 {
		t.Fatalf("dates are inclusive days, got %v %v", q.From, q.To)
	}
	if decode(t, rec)["next"] != "cursor-2" {
		t.Fatalf("body %s", rec.Body.String())
	}
	if rec := serve(t, svc, http.MethodGet, "/whatsapp/forms/0f8c2d1e-0000-4000-8000-000000000001/responses?limit=abc", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a malformed limit is refused, got %d", rec.Code)
	}
}

func TestSendInAConversation(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, http.MethodPost, "/conversations/whatsapp/0f8c2d1e-0000-4000-8000-000000000009/forms", `{"formId":"form-1","copy":{"body":"Oi","cta":"Abrir"}}`)
	if rec.Code != http.StatusCreated || svc.gotSend.EntryID != "0f8c2d1e-0000-4000-8000-000000000009" || svc.gotSend.Copy.CTA != "Abrir" {
		t.Fatalf("status %d send %+v", rec.Code, svc.gotSend)
	}
}

func TestSendableNeedsExactlyOneOfPhoneOrAccount(t *testing.T) {
	svc := &fakeService{}
	if rec := serve(t, svc, http.MethodGet, "/whatsapp/forms/sendable?phoneId=p1", ""); rec.Code != http.StatusOK || svc.gotSendQ.PhoneID != "p1" {
		t.Fatalf("status %d", rec.Code)
	}
	for _, path := range []string{"/whatsapp/forms/sendable", "/whatsapp/forms/sendable?phoneId=p1&metaWabaId=w1"} {
		if rec := serve(t, svc, http.MethodGet, path, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", path, rec.Code)
		}
	}
}

func TestStatsPeriod(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, http.MethodGet, "/whatsapp/forms/0f8c2d1e-0000-4000-8000-000000000001/stats?from=2026-10-01T00:00:00Z", "")
	if rec.Code != http.StatusOK || svc.gotPeriod.From == nil || !svc.gotPeriod.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || svc.gotPeriod.To != nil {
		t.Fatalf("status %d period %+v", rec.Code, svc.gotPeriod)
	}
}

func TestPublishSendsOnlyTheChosenAccounts(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, http.MethodPost, "/whatsapp/forms/0f8c2d1e-0000-4000-8000-000000000001/publish", `{"metaWabaIds":["waba-b"]}`)
	if rec.Code != http.StatusOK || len(svc.gotChosen) != 1 || svc.gotChosen[0] != "waba-b" {
		t.Fatalf("status %d chosen %v", rec.Code, svc.gotChosen)
	}
}

func TestPublishTargetsIsNotReadAsAFormID(t *testing.T) {
	rec := serve(t, &fakeService{}, http.MethodGet, "/whatsapp/forms/publish-targets", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "waba-a") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
