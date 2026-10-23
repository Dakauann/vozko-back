package carriermessaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	"vozko/infra/http/middleware"
	cmuc "vozko/usecases/carriermessaging"
	"vozko/usecases/shared/automationrefs"
)

type fakeAPI struct {
	created    cmuc.SenderInput
	quoteRCS   cmuc.RCSQuote
	quoteSMS   cmuc.SMSQuote
	putErr     error
	delivered  cmuc.DeliverRequest
	deliverErr error
	link       string
	linkErr    error
}

func (f *fakeAPI) ListSenders(context.Context, string) ([]*rcs.Sender, error) { return nil, nil }
func (f *fakeAPI) GetSender(context.Context, string, string) (*rcs.Sender, error) {
	return nil, rcs.ErrSenderNotFound
}
func (f *fakeAPI) CreateSender(_ context.Context, ws string, in cmuc.SenderInput) (*rcs.Sender, error) {
	f.created = in
	return &rcs.Sender{ID: "s1", WorkspaceID: ws, BrandName: in.BrandName, Status: rcs.SenderDraft}, nil
}
func (f *fakeAPI) UpdateSender(context.Context, string, string, cmuc.SenderInput) (*rcs.Sender, error) {
	return nil, rcs.ErrSenderTransition
}
func (f *fakeAPI) SubmitSender(context.Context, string, string) (*rcs.Sender, error) {
	return nil, cmuc.ErrCNPJInvalid
}
func (f *fakeAPI) PauseSender(context.Context, string, string) (*rcs.Sender, error)  { return nil, nil }
func (f *fakeAPI) ResumeSender(context.Context, string, string) (*rcs.Sender, error) { return nil, nil }
func (f *fakeAPI) DeleteSender(context.Context, string, string) error                { return nil }
func (f *fakeAPI) SuggestedDescription(s rcs.Sender) string {
	return "Vozko envia em nome de " + s.BrandName
}
func (f *fakeAPI) ListRoutes(context.Context, string) ([]*sms.Route, error) { return nil, nil }
func (f *fakeAPI) CreateRoute(context.Context, string, string) (*sms.Route, error) {
	return nil, sms.ErrRouteInvalid
}
func (f *fakeAPI) UpdateRoute(context.Context, string, string, string) (*sms.Route, error) {
	return nil, nil
}
func (f *fakeAPI) PauseRoute(context.Context, string, string) (*sms.Route, error)  { return nil, nil }
func (f *fakeAPI) ResumeRoute(context.Context, string, string) (*sms.Route, error) { return nil, nil }
func (f *fakeAPI) DeleteRoute(context.Context, string, string) error               { return nil }
func (f *fakeAPI) QuoteRCS(context.Context, string, rcs.Content) (cmuc.RCSQuote, error) {
	return f.quoteRCS, nil
}
func (f *fakeAPI) QuoteSMS(context.Context, string, string, string) (cmuc.SMSQuote, error) {
	return f.quoteSMS, nil
}
func (f *fakeAPI) GetSettings(_ context.Context, ws string) (carriermessaging.Settings, error) {
	return carriermessaging.DefaultSettings(ws, "America/Sao_Paulo"), nil
}
func (f *fakeAPI) PutSettings(_ context.Context, _ string, s carriermessaging.Settings) (carriermessaging.Settings, error) {
	return s, f.putErr
}
func (f *fakeAPI) ListSuppressions(context.Context, string, int, int) ([]carriermessaging.Suppression, int, error) {
	return []carriermessaging.Suppression{{Phone: "5511999990000", Source: carriermessaging.SuppressionKeyword}}, 1, nil
}
func (f *fakeAPI) AddSuppression(context.Context, string, string) error    { return nil }
func (f *fakeAPI) RemoveSuppression(context.Context, string, string) error { return nil }
func (f *fakeAPI) Deliver(_ context.Context, req cmuc.DeliverRequest) (*carriermessaging.Message, error) {
	f.delivered = req
	if f.deliverErr != nil {
		return nil, f.deliverErr
	}
	return &carriermessaging.Message{ID: "m1", Channel: req.Channel, Status: carriermessaging.StatusAccepted}, nil
}
func (f *fakeAPI) ResolveLinkClick(context.Context, string, time.Time) (string, error) {
	return f.link, f.linkErr
}
func (f *fakeAPI) AdminListSenders(context.Context, *rcs.SenderStatus) ([]*rcs.Sender, error) {
	return nil, nil
}
func (f *fakeAPI) AdminPauseSender(context.Context, string, rcs.PauseReason) (*rcs.Sender, error) {
	return nil, nil
}
func (f *fakeAPI) AdminResumeSender(context.Context, string) (*rcs.Sender, error) { return nil, nil }
func (f *fakeAPI) AdminListRoutes(context.Context) ([]*sms.Route, error)          { return nil, nil }
func (f *fakeAPI) AdminAssignNumber(context.Context, string, string) (*sms.Route, error) {
	return nil, cmuc.ErrNumberTaken
}
func (f *fakeAPI) AdminReleaseNumber(context.Context, string) (*sms.Route, error) { return nil, nil }

func call(t *testing.T, handler http.HandlerFunc, method, path, pattern string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	router := mux.NewRouter()
	router.HandleFunc(pattern, handler)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func dataOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if data, ok := body["data"].(map[string]any); ok {
		return data
	}
	return body
}

func TestCreateSenderReturnsTheSuggestedDescription(t *testing.T) {
	api := &fakeAPI{}
	h := NewHandler(api)
	rec := call(t, h.CreateSender, http.MethodPost, "/rcs/senders", "/rcs/senders", SenderRequest{BrandName: "Loja", UseCase: "promotional", BillingCategory: "conversational"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
	data := dataOf(t, rec)
	if data["suggested_description"] != "Vozko envia em nome de Loja" || data["status"] != "draft" || data["editable"] != true {
		t.Fatalf("data = %+v", data)
	}
	if api.created.UseCase != rcs.UseCasePromotional || api.created.BillingCategory != rcs.BillingConversational {
		t.Fatalf("input = %+v", api.created)
	}
}

func TestCreateSenderRefusesUnknownFields(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).CreateSender, http.MethodPost, "/rcs/senders", "/rcs/senders", map[string]any{"brand": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestSenderRefusalsCarryTheirCodes(t *testing.T) {
	h := NewHandler(&fakeAPI{})
	rec := call(t, h.SubmitSender, http.MethodPost, "/rcs/senders/s1/submit", "/rcs/senders/{id}/submit", nil)
	if rec.Code != http.StatusUnprocessableEntity || decodeError(t, rec)["code"] != "rcs_cnpj_invalid" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
	rec = call(t, h.GetSender, http.MethodGet, "/rcs/senders/x", "/rcs/senders/{id}", nil)
	if rec.Code != http.StatusNotFound || decodeError(t, rec)["code"] != "rcs_sender_not_found" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestContentCheckTranslatesIssuesIntoCodesAndPaths(t *testing.T) {
	api := &fakeAPI{quoteRCS: cmuc.RCSQuote{
		Kind: rcs.KindText, Class: rcs.ClassSingle, Charge: carriermessaging.ChargeRCSSingle, UnitMicros: 63_333,
		Issues: []rcs.Issue{{Path: "suggestions[0]", Err: rcs.ErrSuggestionInvalid}},
	}}
	rec := call(t, NewHandler(api).CheckContent, http.MethodPost, "/rcs/content/check", "/rcs/content/check", ContentCheckRequest{Content: rcs.Content{Text: "Oi"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var out ContentCheckResponse
	raw, _ := json.Marshal(dataOf(t, rec))
	_ = json.Unmarshal(raw, &out)
	if out.BillingClass != "single" || len(out.Issues) != 1 || out.Issues[0].Code != "rcs_button_invalid" || out.Issues[0].Path != "suggestions[0]" || out.Price.UnitMicros != 63_333 {
		t.Fatalf("out = %+v", out)
	}
}

func TestTextCheckReportsTheMeasurement(t *testing.T) {
	api := &fakeAPI{quoteSMS: cmuc.SMSQuote{
		Text: "Loja: Oi", Measurement: sms.Measure("Loja: Oi"), Charge: carriermessaging.ChargeSMSSegment, UnitMicros: 35_000, TotalMicros: 35_000,
		Issues: []error{sms.ErrTextTooLong},
	}}
	rec := call(t, NewHandler(api).CheckText, http.MethodPost, "/sms/text/check", "/sms/text/check", TextCheckRequest{Text: "Oi", RouteID: "r1"})
	data := dataOf(t, rec)
	measurement, _ := data["measurement"].(map[string]any)
	issues, _ := data["issues"].([]any)
	if rec.Code != http.StatusOK || data["composed_text"] != "Loja: Oi" || measurement["segments"] != float64(1) || len(issues) != 1 {
		t.Fatalf("data = %+v", data)
	}
}

func TestToGSM(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).ToGSM, http.MethodPost, "/sms/text/to-gsm", "/sms/text/to-gsm", ToGSMRequest{Text: "Promoção"})
	if dataOf(t, rec)["text"] != "Promocao" {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestPutSettingsRefusal(t *testing.T) {
	api := &fakeAPI{putErr: carriermessaging.ErrSettingsInvalid}
	rec := call(t, NewHandler(api).PutSettings, http.MethodPut, "/carrier/settings", "/carrier/settings", carriermessaging.DefaultSettings("ws-1", "America/Sao_Paulo"))
	if rec.Code != http.StatusUnprocessableEntity || decodeError(t, rec)["code"] != "carrier_settings_invalid" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestTestSendActsAsThePersonWithTheTestRunKind(t *testing.T) {
	api := &fakeAPI{}
	rec := call(t, NewHandler(api).TestSend, http.MethodPost, "/carrier/test-sends", "/carrier/test-sends", TestSendRequest{Channel: "sms", SenderID: "r1", Phone: "5511999990000", Text: "Oi"})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
	if api.delivered.RunKind != carriermessaging.RunTest || api.delivered.Marketing || api.delivered.Channel != carriermessaging.ChannelSMS || api.delivered.WorkspaceID != "ws-1" {
		t.Fatalf("delivered = %+v", api.delivered)
	}
	api.deliverErr = &carriermessaging.ShortfallError{MissingMicros: 10}
	rec = call(t, NewHandler(api).TestSend, http.MethodPost, "/carrier/test-sends", "/carrier/test-sends", TestSendRequest{Channel: "sms", SenderID: "r1", Phone: "5511999990000", Text: "Oi"})
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestFollowLinkRedirectsOrHides(t *testing.T) {
	api := &fakeAPI{link: "https://loja.example.com/p"}
	rec := call(t, NewHandler(api).FollowLink, http.MethodGet, "/public/carrier/r/tok", "/public/carrier/r/{token}", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://loja.example.com/p" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code = %d, headers = %+v", rec.Code, rec.Header())
	}
	api.linkErr = errors.New("bad token")
	rec = call(t, NewHandler(api).FollowLink, http.MethodGet, "/public/carrier/r/tok", "/public/carrier/r/{token}", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d", rec.Code)
	}
}

func (f *fakeAPI) ResolveMedia(context.Context, string, string) (*rcs.Media, error) {
	return &rcs.Media{Kind: rcs.MediaImage, URL: "https://cdn.example.com/a.jpg", MIMEType: "image/jpeg"}, nil
}

func TestIncompleteSenderRefusalNamesTheField(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, &rcs.IncompleteError{Field: "logo_url"}, "test")
	body := decodeError(t, rec)
	details, _ := body["details"].(map[string]any)
	if rec.Code != http.StatusUnprocessableEntity || body["code"] != "rcs_sender_incomplete" || details["field"] != "logo_url" {
		t.Fatalf("body = %+v", body)
	}
}

func TestResolveMediaEndpoint(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).ResolveMedia, http.MethodPost, "/rcs/media", "/rcs/media", MediaRequest{MediaID: "m1"})
	if rec.Code != http.StatusOK || dataOf(t, rec)["kind"] != "image" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func (f *fakeAPI) SetSenderInbox(context.Context, string, string, cmuc.InboxSettings) (*rcs.Sender, error) {
	return nil, cmuc.ErrAutomationIncomplete
}
func (f *fakeAPI) SetRouteInbox(_ context.Context, _, id string, in cmuc.InboxSettings) (*sms.Route, error) {
	return &sms.Route{ID: id, DepartmentID: in.DepartmentID, PipelineID: in.PipelineID, Automation: in.Automation}, nil
}

func TestInboxRefusal(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).SetSenderInbox, http.MethodPut, "/rcs/senders/s1/inbox", "/rcs/senders/{id}/inbox", InboxRequest{Automation: AutomationDTO{EnableWorkflow: true}})
	if rec.Code != http.StatusUnprocessableEntity || decodeError(t, rec)["code"] != "carrier_automation_incomplete" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestRouteInboxCarriesDepartmentAndFunnel(t *testing.T) {
	dept := "dept-1"
	rec := call(t, NewHandler(&fakeAPI{}).SetRouteInbox, http.MethodPut, "/sms/routes/r1/inbox", "/sms/routes/{id}/inbox", InboxRequest{DepartmentID: &dept})
	data := dataOf(t, rec)
	if rec.Code != http.StatusOK || data["department_id"] != "dept-1" || data["pipeline_id"] != nil {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestInboxRefsOutsideTheWorkspaceAreRefused(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, automationrefs.ErrNotInWorkspace, "inbox")
	if rec.Code != http.StatusUnprocessableEntity || decodeError(t, rec)["code"] != "carrier_inbox_refs_not_in_workspace" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestChannelsSayWhichAreLaunched(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).ListChannels, http.MethodGet, "/carrier/channels", "/carrier/channels", nil)
	channels, _ := dataOf(t, rec)["channels"].([]any)
	got := map[string]bool{}
	for _, c := range channels {
		entry := c.(map[string]any)
		got[entry["channel"].(string)] = entry["launched"].(bool)
	}
	if rec.Code != http.StatusOK || len(got) != 2 || got["rcs"] != carriermessaging.ChannelRCS.Launched() || got["sms"] != carriermessaging.ChannelSMS.Launched() {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestComingSoonRefusal(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, carriermessaging.ErrChannelNotLaunched, "send")
	if rec.Code != http.StatusConflict || decodeError(t, rec)["code"] != "carrier_channel_coming_soon" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}
