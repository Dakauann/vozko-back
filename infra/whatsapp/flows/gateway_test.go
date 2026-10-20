package flows

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/whatsappform"
	"vozko/infra/meta"
)

type recordedCall struct {
	method      string
	path        string
	query       url.Values
	form        url.Values
	auth        string
	fileField   string
	fileName    string
	fileType    string
	fileContent string
}

type stub struct {
	mu      sync.Mutex
	calls   []recordedCall
	baseURL string
}

func (s *stub) recorded() []recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedCall(nil), s.calls...)
}

func record(t *testing.T, r *http.Request) recordedCall {
	t.Helper()
	call := recordedCall{method: r.Method, path: r.URL.Path, query: r.URL.Query(), auth: r.Header.Get("Authorization")}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return call
		}
		call.form = url.Values(r.MultipartForm.Value)
		for field, files := range r.MultipartForm.File {
			call.fileField = field
			call.fileName = files[0].Filename
			call.fileType = files[0].Header.Get("Content-Type")
			opened, err := files[0].Open()
			if err != nil {
				t.Errorf("open file part: %v", err)
				return call
			}
			content, _ := io.ReadAll(opened)
			_ = opened.Close()
			call.fileContent = string(content)
		}
		return call
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		call.form = r.PostForm
	}
	return call
}

func gatewayAgainst(t *testing.T, respond func(s *stub, call recordedCall) (int, string)) (*Gateway, *stub) {
	t.Helper()
	s := &stub{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := record(t, r)
		s.mu.Lock()
		s.calls = append(s.calls, call)
		s.mu.Unlock()
		status, body := respond(s, call)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	s.baseURL = server.URL
	client, err := meta.NewClient(meta.Config{Host: strings.TrimPrefix(server.URL, "https://"), APIVersion: meta.DefaultGraphVersion, MaxRetries: 1, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return NewGateway(client), s
}

func reply(status int, body string) func(*stub, recordedCall) (int, string) {
	return func(*stub, recordedCall) (int, string) { return status, body }
}

var account = whatsappform.FlowAccount{MetaWABAID: "waba1", AccessToken: "tok"}

const version = "/" + meta.DefaultGraphVersion

func TestCreateSendsTheFlowAsFormFields(t *testing.T) {
	cases := []struct {
		name        string
		input       whatsappform.CreateFlowInput
		wantForm    url.Values
		wantMissing []string
	}{
		{
			name:  "publish with json",
			input: whatsappform.CreateFlowInput{Name: "Cadastro · Vozko 1a2b3c4d", Categories: []whatsappform.Category{whatsappform.CategoryLeadGeneration, whatsappform.CategorySurvey}, JSON: []byte(`{"version":"7.3"}`), Publish: true},
			wantForm: url.Values{
				"name":       {"Cadastro · Vozko 1a2b3c4d"},
				"categories": {`["LEAD_GENERATION","SURVEY"]`},
				"flow_json":  {`{"version":"7.3"}`},
				"publish":    {"true"},
			},
		},
		{
			name:        "draft without json",
			input:       whatsappform.CreateFlowInput{Name: "Rascunho", Categories: []whatsappform.Category{whatsappform.CategoryOther}},
			wantForm:    url.Values{"name": {"Rascunho"}, "categories": {`["OTHER"]`}, "publish": {"false"}},
			wantMissing: []string{"flow_json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, s := gatewayAgainst(t, reply(http.StatusOK, `{"id":"F1","success":true,"validation_errors":[]}`))

			flow, err := gateway.Create(context.Background(), account, tc.input)

			if err != nil {
				t.Fatal(err)
			}
			if flow.ID != "F1" || flow.Name != tc.input.Name || len(flow.ValidationErrors) != 0 {
				t.Fatalf("flow = %+v", flow)
			}
			calls := s.recorded()
			if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != version+"/waba1/flows" || calls[0].auth != "Bearer tok" {
				t.Fatalf("calls = %+v", calls)
			}
			for key, want := range tc.wantForm {
				if got := calls[0].form[key]; !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %v, want %v", key, got, want)
				}
			}
			for _, key := range tc.wantMissing {
				if _, present := calls[0].form[key]; present {
					t.Errorf("%s must not be sent", key)
				}
			}
		})
	}
}

func TestValidationErrorsInASuccessfulReplyAreReturnedNotFailed(t *testing.T) {
	body := `{"id":"F1","success":true,"validation_errors":[
		{"error":"INVALID_PROPERTY_VALUE","error_type":"FLOW_JSON_ERROR","message":"Invalid value found for property 'label'.","line_start":10,"line_end":10,"column_start":21,"column_end":34,
		 "pointers":[{"line_start":10,"line_end":10,"column_start":21,"column_end":34,"path":"screens[0].layout.children[1].label"}]},
		{"error":"MISSING_REQUIRED_PROPERTY","error_type":"FLOW_JSON_ERROR","message":"","pointers":[]}
	]}`
	gateway, _ := gatewayAgainst(t, reply(http.StatusOK, body))

	flow, err := gateway.Create(context.Background(), account, whatsappform.CreateFlowInput{Name: "Cadastro", Categories: []whatsappform.Category{whatsappform.CategoryOther}, JSON: []byte(`{}`)})

	if err != nil {
		t.Fatal(err)
	}
	want := []whatsappform.MetaValidationError{
		{Message: "Invalid value found for property 'label'.", Paths: []string{"screens[0].layout.children[1].label"}},
		{Message: "MISSING_REQUIRED_PROPERTY"},
	}
	if !reflect.DeepEqual(flow.ValidationErrors, want) {
		t.Fatalf("validation errors = %#v", flow.ValidationErrors)
	}
}

func TestCreateWithoutAFlowIDIsAnError(t *testing.T) {
	gateway, _ := gatewayAgainst(t, reply(http.StatusOK, `{"success":false}`))

	_, err := gateway.Create(context.Background(), account, whatsappform.CreateFlowInput{Name: "Cadastro", Categories: []whatsappform.Category{whatsappform.CategoryOther}})

	if err == nil {
		t.Fatal("a create Meta did not confirm must not look like a flow")
	}
}

func TestMetaErrorsAreRejectionsOrPlainErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantReject bool
		wantCode   int
		wantCalls  int
	}{
		{name: "name not unique", status: http.StatusBadRequest, body: `{"error":{"message":"Flow name is not unique","type":"OAuthException","code":100,"error_subcode":4016019}}`, wantReject: true, wantCode: 100, wantCalls: 1},
		{name: "blocked by integrity", status: http.StatusBadRequest, body: `{"error":{"message":"Blocked by integrity","code":139000}}`, wantReject: true, wantCode: 139000, wantCalls: 1},
		{name: "flows code with server status", status: http.StatusInternalServerError, body: `{"error":{"message":"Can not update","code":139001}}`, wantReject: true, wantCode: 139001, wantCalls: 1},
		{name: "permission", status: http.StatusForbidden, body: `{"error":{"message":"Application does not have permission","code":10}}`, wantReject: true, wantCode: 10, wantCalls: 1},
		{name: "server error", status: http.StatusInternalServerError, body: `{"error":{"message":"Please retry","code":131000}}`, wantCalls: 1},
		{name: "transient", status: http.StatusServiceUnavailable, body: `{"error":{"message":"Temporary","code":2,"is_transient":true}}`, wantCalls: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, s := gatewayAgainst(t, reply(tc.status, tc.body))

			_, err := gateway.Create(context.Background(), account, whatsappform.CreateFlowInput{Name: "Cadastro", Categories: []whatsappform.Category{whatsappform.CategoryOther}})

			if err == nil {
				t.Fatal("expected an error")
			}
			var rejection *whatsappform.MetaRejection
			isRejection := errors.As(err, &rejection)
			if isRejection != tc.wantReject || errors.Is(err, whatsappform.ErrMetaRejected) != tc.wantReject {
				t.Fatalf("rejection = %v for %v", isRejection, err)
			}
			if tc.wantReject && (rejection.Code != tc.wantCode || rejection.Message == "") {
				t.Fatalf("rejection = %+v", rejection)
			}
			if got := len(s.recorded()); got != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestUploadJSONSendsTheFlowAsAMultipartAsset(t *testing.T) {
	gateway, s := gatewayAgainst(t, reply(http.StatusOK, `{"success":true,"validation_errors":[{"error":"INVALID","message":"Bad screen","pointers":[{"path":"screens[1]"},{"path":""}]}]}`))

	problems, err := gateway.UploadJSON(context.Background(), account, "F1", []byte(`{"version":"7.3","screens":[]}`))

	if err != nil {
		t.Fatal(err)
	}
	if want := []whatsappform.MetaValidationError{{Message: "Bad screen", Paths: []string{"screens[1]"}}}; !reflect.DeepEqual(problems, want) {
		t.Fatalf("problems = %#v", problems)
	}
	call := s.recorded()[0]
	if call.method != http.MethodPost || call.path != version+"/F1/assets" || call.auth != "Bearer tok" {
		t.Fatalf("call = %+v", call)
	}
	if call.fileField != "file" || call.fileName != "flow.json" || call.fileType != "application/json" || call.fileContent != `{"version":"7.3","screens":[]}` {
		t.Fatalf("file part = %+v", call)
	}
	if call.form.Get("name") != "flow.json" || call.form.Get("asset_type") != "FLOW_JSON" {
		t.Fatalf("form = %v", call.form)
	}
}

func TestUploadJSONRefusedByMetaIsARejection(t *testing.T) {
	gateway, _ := gatewayAgainst(t, reply(http.StatusBadRequest, `{"error":{"message":"Can not update a published flow","code":139001}}`))

	_, err := gateway.UploadJSON(context.Background(), account, "F1", []byte(`{}`))

	var rejection *whatsappform.MetaRejection
	if !errors.As(err, &rejection) || rejection.Code != 139001 {
		t.Fatalf("err = %v", err)
	}
}

func TestPublishAndDeprecatePostToTheFlow(t *testing.T) {
	cases := []struct {
		name string
		call func(*Gateway) error
		path string
	}{
		{name: "publish", call: func(g *Gateway) error { return g.Publish(context.Background(), account, "F1") }, path: version + "/F1/publish"},
		{name: "deprecate", call: func(g *Gateway) error { return g.Deprecate(context.Background(), account, "F1") }, path: version + "/F1/deprecate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, s := gatewayAgainst(t, reply(http.StatusOK, `{"success":true}`))

			if err := tc.call(gateway); err != nil {
				t.Fatal(err)
			}
			calls := s.recorded()
			if len(calls) != 1 || calls[0].method != http.MethodPost || calls[0].path != tc.path || calls[0].auth != "Bearer tok" {
				t.Fatalf("calls = %+v", calls)
			}
		})
		t.Run(tc.name+" unconfirmed", func(t *testing.T) {
			gateway, _ := gatewayAgainst(t, reply(http.StatusOK, `{"success":false}`))

			if err := tc.call(gateway); err == nil {
				t.Fatal("an unconfirmed change must not look done")
			}
		})
	}
}

func TestPublishInAnInvalidStateIsARejection(t *testing.T) {
	gateway, _ := gatewayAgainst(t, reply(http.StatusBadRequest, `{"error":{"message":"Publishing Flow in invalid state","code":139002}}`))

	err := gateway.Publish(context.Background(), account, "F1")

	var rejection *whatsappform.MetaRejection
	if !errors.As(err, &rejection) || rejection.Code != 139002 {
		t.Fatalf("err = %v", err)
	}
}

func TestGetReadsTheFlowWithItsPreview(t *testing.T) {
	gateway, s := gatewayAgainst(t, reply(http.StatusOK, `{"id":"F1","name":"Cadastro","status":"PUBLISHED","categories":["OTHER"],"json_version":"7.3",
		"validation_errors":[{"error":"WARN","message":"Deprecated component","pointers":[{"path":"screens[0]"}]}],
		"preview":{"preview_url":"https://business.facebook.com/wa/manage/flows/F1/preview/?token=abc","expires_at":"2026-11-18T11:18:09+0000"}}`))

	flow, err := gateway.Get(context.Background(), account, "F1")

	if err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2026, 11, 18, 11, 18, 9, 0, time.UTC)
	want := whatsappform.RemoteFlow{
		ID: "F1", Name: "Cadastro", Status: "PUBLISHED",
		ValidationErrors: []whatsappform.MetaValidationError{{Message: "Deprecated component", Paths: []string{"screens[0]"}}},
		PreviewURL:       "https://business.facebook.com/wa/manage/flows/F1/preview/?token=abc",
		PreviewExpiresAt: &expires,
	}
	if flow.PreviewExpiresAt == nil || !flow.PreviewExpiresAt.Equal(expires) {
		t.Fatalf("expires = %v", flow.PreviewExpiresAt)
	}
	flow.PreviewExpiresAt = &expires
	if !reflect.DeepEqual(flow, want) {
		t.Fatalf("flow = %#v", flow)
	}
	call := s.recorded()[0]
	if call.method != http.MethodGet || call.path != version+"/F1" || call.query.Get("fields") != "id,name,status,categories,validation_errors,json_version,preview.invalidate(false)" {
		t.Fatalf("call = %+v", call)
	}
}

func TestGetWithAnUnreadableExpiryIsAnError(t *testing.T) {
	gateway, _ := gatewayAgainst(t, reply(http.StatusOK, `{"id":"F1","status":"DRAFT","preview":{"preview_url":"https://x","expires_at":"tomorrow"}}`))

	if _, err := gateway.Get(context.Background(), account, "F1"); err == nil {
		t.Fatal("an expiry Meta wrote in an unknown shape must not be guessed")
	}
}

func TestGetRetriesTransientFailuresAndStaysAPlainError(t *testing.T) {
	gateway, s := gatewayAgainst(t, reply(http.StatusInternalServerError, `{"error":{"message":"Service unavailable","code":2}}`))

	_, err := gateway.Get(context.Background(), account, "F1")

	if err == nil || errors.Is(err, whatsappform.ErrMetaRejected) {
		t.Fatalf("err = %v", err)
	}
	if len(s.recorded()) != 2 {
		t.Fatalf("calls = %d", len(s.recorded()))
	}
}

func TestPreviewAsksForAFreshLinkOnlyWhenRefreshing(t *testing.T) {
	cases := []struct {
		refresh bool
		fields  string
	}{
		{refresh: false, fields: "preview.invalidate(false)"},
		{refresh: true, fields: "preview.invalidate(true)"},
	}
	for _, tc := range cases {
		t.Run(tc.fields, func(t *testing.T) {
			gateway, s := gatewayAgainst(t, reply(http.StatusOK, `{"id":"F1","preview":{"preview_url":"https://preview/F1","expires_at":"2026-11-18T11:18:09+0000"}}`))

			flow, err := gateway.Preview(context.Background(), account, "F1", tc.refresh)

			if err != nil {
				t.Fatal(err)
			}
			if flow.ID != "F1" || flow.PreviewURL != "https://preview/F1" || flow.PreviewExpiresAt == nil {
				t.Fatalf("flow = %+v", flow)
			}
			call := s.recorded()[0]
			if call.path != version+"/F1" || call.query.Get("fields") != tc.fields {
				t.Fatalf("call = %+v", call)
			}
		})
	}
}

func TestPreviewWithoutALinkIsAnError(t *testing.T) {
	gateway, _ := gatewayAgainst(t, reply(http.StatusOK, `{"id":"F1"}`))

	if _, err := gateway.Preview(context.Background(), account, "F1", false); err == nil {
		t.Fatal("a preview without a link must not look ready")
	}
}

func pagedFlows(pages map[string]string) func(*stub, recordedCall) (int, string) {
	return func(_ *stub, call recordedCall) (int, string) {
		body, ok := pages[call.query.Get("after")]
		if !ok {
			return http.StatusBadRequest, `{"error":{"message":"unexpected cursor","code":100}}`
		}
		return http.StatusOK, body
	}
}

var twoPages = map[string]string{
	"":   `{"data":[{"id":"F1","name":"Outro","status":"DRAFT"},{"id":"F2","name":"cadastro","status":"PUBLISHED"}],"paging":{"cursors":{"after":"C1"},"next":"https://graph.facebook.com/next"}}`,
	"C1": `{"data":[{"id":"F3","name":"Cadastro","status":"PUBLISHED"}],"paging":{"cursors":{"after":"C2"}}}`,
}

func TestFindByNameWalksThePagesForTheExactName(t *testing.T) {
	cases := []struct {
		name      string
		wantID    string
		wantCalls int
	}{
		{name: "Cadastro", wantID: "F3", wantCalls: 2},
		{name: "Outro", wantID: "F1", wantCalls: 1},
		{name: "Ausente", wantCalls: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, s := gatewayAgainst(t, pagedFlows(twoPages))

			flow, err := gateway.FindByName(context.Background(), account, tc.name)

			if err != nil {
				t.Fatal(err)
			}
			if tc.wantID == "" && flow != nil {
				t.Fatalf("flow = %+v", flow)
			}
			if tc.wantID != "" && (flow == nil || flow.ID != tc.wantID || flow.Name != tc.name) {
				t.Fatalf("flow = %+v", flow)
			}
			calls := s.recorded()
			if len(calls) != tc.wantCalls {
				t.Fatalf("calls = %d", len(calls))
			}
			if calls[0].path != version+"/waba1/flows" || calls[0].query.Get("fields") != "id,name,status" || calls[0].auth != "Bearer tok" {
				t.Fatalf("first call = %+v", calls[0])
			}
		})
	}
}

func TestListReturnsEveryPage(t *testing.T) {
	gateway, s := gatewayAgainst(t, pagedFlows(twoPages))

	flows, err := gateway.List(context.Background(), account)

	if err != nil {
		t.Fatal(err)
	}
	want := []whatsappform.RemoteFlow{
		{ID: "F1", Name: "Outro", Status: "DRAFT"},
		{ID: "F2", Name: "cadastro", Status: "PUBLISHED"},
		{ID: "F3", Name: "Cadastro", Status: "PUBLISHED"},
	}
	if !reflect.DeepEqual(flows, want) {
		t.Fatalf("flows = %#v", flows)
	}
	if calls := s.recorded(); len(calls) != 2 || calls[1].query.Get("after") != "C1" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestPagingThatNeverEndsIsAnError(t *testing.T) {
	gateway, _ := gatewayAgainst(t, reply(http.StatusOK, `{"data":[],"paging":{"next":"https://graph.facebook.com/next"}}`))

	if _, err := gateway.List(context.Background(), account); err == nil {
		t.Fatal("a next page without a cursor must not end the list silently")
	}
}

func TestDownloadJSONFetchesTheFlowJSONAsset(t *testing.T) {
	gateway, s := gatewayAgainst(t, func(s *stub, call recordedCall) (int, string) {
		switch call.path {
		case version + "/F1/assets":
			return http.StatusOK, `{"data":[{"name":"image.png","asset_type":"IMAGE","download_url":"` + s.baseURL + `/cdn/image.png"},
				{"name":"flow.json","asset_type":"FLOW_JSON","download_url":"` + s.baseURL + `/cdn/flow.json?sig=1"}],"paging":{"cursors":{"after":"A"}}}`
		case "/cdn/flow.json":
			return http.StatusOK, `{"version":"7.3","screens":[]}`
		}
		return http.StatusNotFound, `{"error":{"message":"unexpected","code":100}}`
	})

	raw, err := gateway.DownloadJSON(context.Background(), account, "F1")

	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"version":"7.3","screens":[]}` {
		t.Fatalf("raw = %s", raw)
	}
	calls := s.recorded()
	if len(calls) != 2 || calls[1].path != "/cdn/flow.json" || calls[1].query.Get("sig") != "1" || calls[1].auth != "Bearer tok" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestDownloadJSONRefusals(t *testing.T) {
	big := `"` + strings.Repeat("a", maxFlowJSONBytes) + `"`
	cases := []struct {
		name   string
		assets func(base string) string
		file   string
	}{
		{name: "no flow json asset", assets: func(base string) string {
			return `{"data":[{"name":"image.png","asset_type":"IMAGE","download_url":"` + base + `/cdn/image.png"}]}`
		}},
		{name: "plain http link", assets: func(string) string {
			return `{"data":[{"name":"flow.json","asset_type":"FLOW_JSON","download_url":"http://cdn.example/flow.json"}]}`
		}},
		{name: "too large", assets: func(base string) string {
			return `{"data":[{"name":"flow.json","asset_type":"FLOW_JSON","download_url":"` + base + `/cdn/flow.json"}]}`
		}, file: big},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway, _ := gatewayAgainst(t, func(s *stub, call recordedCall) (int, string) {
				if call.path == "/cdn/flow.json" {
					return http.StatusOK, tc.file
				}
				return http.StatusOK, tc.assets(s.baseURL)
			})

			if _, err := gateway.DownloadJSON(context.Background(), account, "F1"); err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}
}
