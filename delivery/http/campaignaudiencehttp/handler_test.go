package campaignaudiencehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

type fakeAPI struct {
	channel  campaignaudience.Channel
	params   campaignaudience_usecase.Params
	spec     campaignaudience.Spec
	upload   campaignaudience_usecase.Upload
	view     campaignaudience_usecase.View
	query    campaignaudience_usecase.RowsQuery
	leads    campaignaudience_usecase.LeadsQuery
	row      campaignaudience_usecase.RowInput
	ids      []string
	err      error
	sample   campaignaudience_usecase.File
	saved    []campaignaudience.Suggestion
	actorIDs []string
}

func (f *fakeAPI) seen(a campaignaudience_usecase.Actor, ch campaignaudience.Channel) {
	f.actorIDs = append(f.actorIDs, a.UserID)
	f.channel = ch
}

func (f *fakeAPI) Count(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, p campaignaudience_usecase.Params, s campaignaudience.Spec) (campaignaudience.Count, error) {
	f.seen(a, ch)
	f.params, f.spec = p, s
	sendable := 3
	return campaignaudience.Count{Channel: ch, Matched: 4, Sendable: &sendable, Out: map[string]int{"blocked": 1}}, f.err
}

func (f *fakeAPI) Leads(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, p campaignaudience_usecase.Params, s campaignaudience.Spec, q campaignaudience_usecase.LeadsQuery) (campaignaudience_usecase.LeadsPage, error) {
	f.seen(a, ch)
	f.params, f.spec, f.leads = p, s, q
	return campaignaudience_usecase.LeadsPage{Items: []campaignaudience.AudienceLead{{LeadID: "lead-1", Status: campaignaudience.LeadOut, Reason: "no_email"}}, Page: q.Page, Limit: q.Limit, Total: 3}, f.err
}

func (f *fakeAPI) Upload(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, up campaignaudience_usecase.Upload, v campaignaudience_usecase.View) (campaignaudience_usecase.Preview, error) {
	f.seen(a, ch)
	f.upload, f.view = up, v
	return campaignaudience_usecase.Preview{ID: "list-1", Rows: 1}, f.err
}

func (f *fakeAPI) Preview(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID string, v campaignaudience_usecase.View) (campaignaudience_usecase.Preview, error) {
	f.seen(a, ch)
	f.ids, f.view = []string{listID}, v
	return campaignaudience_usecase.Preview{ID: listID}, f.err
}

func (f *fakeAPI) Rows(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID string, q campaignaudience_usecase.RowsQuery) (campaignaudience_usecase.RowPage, error) {
	f.seen(a, ch)
	f.ids, f.query = []string{listID}, q
	return campaignaudience_usecase.RowPage{Items: []campaignaudience_usecase.RowView{{ID: "r1"}}, Page: q.Page, Limit: q.Limit, Total: 3}, f.err
}

func (f *fakeAPI) AddRow(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID string, in campaignaudience_usecase.RowInput) (campaignaudience.Row, error) {
	f.seen(a, ch)
	f.ids, f.row = []string{listID}, in
	return campaignaudience.Row{ID: "r1"}, f.err
}

func (f *fakeAPI) EditRow(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID, rowID string, in campaignaudience_usecase.RowInput) (campaignaudience.Row, error) {
	f.seen(a, ch)
	f.ids, f.row = []string{listID, rowID}, in
	return campaignaudience.Row{ID: rowID}, f.err
}

func (f *fakeAPI) RemoveRow(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID, rowID string) error {
	f.seen(a, ch)
	f.ids = []string{listID, rowID}
	return f.err
}

func (f *fakeAPI) Sample(_ context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, p campaignaudience_usecase.Params, _ []campaignaudience.Variable, _ campaignaudience_usecase.SampleFormat) (campaignaudience_usecase.File, error) {
	f.seen(a, ch)
	f.params = p
	return f.sample, f.err
}

func (f *fakeAPI) Fields(_ context.Context, a campaignaudience_usecase.Actor) ([]campaignaudience_usecase.Field, error) {
	f.seen(a, "")
	return []campaignaudience_usecase.Field{{Source: "lead.first_name"}}, f.err
}

func (f *fakeAPI) TemplateSuggestions(_ context.Context, a campaignaudience_usecase.Actor, id string) (campaignaudience_usecase.TemplateSuggestions, error) {
	f.seen(a, "")
	f.ids = []string{id}
	return campaignaudience_usecase.TemplateSuggestions{TemplateID: id}, f.err
}

func (f *fakeAPI) SaveTemplateSuggestions(_ context.Context, a campaignaudience_usecase.Actor, id string, s []campaignaudience.Suggestion) (campaignaudience_usecase.TemplateSuggestions, error) {
	f.seen(a, "")
	f.ids, f.saved = []string{id}, s
	return campaignaudience_usecase.TemplateSuggestions{TemplateID: id, Suggestions: s}, f.err
}

type guard struct {
	resource workspace.Resource
	action   workspace.Action
}

func server(t *testing.T, api *fakeAPI) (*mux.Router, *[]guard) {
	t.Helper()
	var guards []guard
	router := mux.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
			ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	RegisterRoutes(router, NewHandler(api), func(res workspace.Resource, action workspace.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			guards = append(guards, guard{res, action})
			next(w, r)
		}
	})
	return router, &guards
}

func call(router http.Handler, method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestEveryChannelIsGuardedByItsOwnResource(t *testing.T) {
	api := &fakeAPI{}
	router, guards := server(t, api)
	cases := map[string]workspace.Resource{
		"email": workspace.ResourceEmailMarketing, "whatsapp": workspace.ResourceWhatsAppCampaigns,
		"unofficial": workspace.ResourceUnofficialWhatsAppCampaigns, "carrier": workspace.ResourceCarrierMessaging,
	}
	for channel, resource := range cases {
		*guards = nil
		rec := call(router, http.MethodPost, "/campaign-audience/"+channel+"/count", []byte(`{"source":"leads","selection":{"mode":"everyone"}}`), "application/json")
		if rec.Code != http.StatusOK || len(*guards) != 1 || (*guards)[0] != (guard{resource, workspace.ActionCreate}) || api.channel != campaignaudience.Channel(channel) {
			t.Fatalf("%s: %d %+v %s", channel, rec.Code, *guards, rec.Body)
		}
		*guards = nil
		if rec := call(router, http.MethodGet, "/campaign-audience/"+channel+"/fields", nil, ""); rec.Code != http.StatusOK || (*guards)[0] != (guard{resource, workspace.ActionRead}) {
			t.Fatalf("%s fields: %d %+v", channel, rec.Code, *guards)
		}
	}
	if rec := call(router, http.MethodPost, "/campaign-audience/sms/count", []byte(`{}`), "application/json"); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown channel = %d", rec.Code)
	}
	*guards = nil
	if rec := call(router, http.MethodGet, "/campaign-audience/template-fields", nil, ""); rec.Code != http.StatusOK || (*guards)[0] != (guard{workspace.ResourceWhatsAppTemplates, workspace.ActionRead}) {
		t.Fatalf("template fields = %d %+v", rec.Code, *guards)
	}
}

func TestCountReadsTheSpecAndTheChannelFields(t *testing.T) {
	api := &fakeAPI{}
	router, _ := server(t, api)
	body := `{"source":"list","listId":"list-1","variables":[{"key":"1","source":"lead.first_name","missing":"exclude"}],"templateId":" tpl-1 ","businessPhoneId":"phone-1","message":{"kind":"text","bodies":["Oi {{1}}"]},"subject":"Oi","document":{"version":1,"blocks":[]},"workflowId":" wf-1 ","agentId":"agent-1"}`
	rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/count", []byte(body), "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("count = %d %s", rec.Code, rec.Body)
	}
	if api.spec.Source != campaignaudience.SourceList || api.spec.ListID != "list-1" || api.spec.Variables[0].Missing != campaignaudience.MissingExclude {
		t.Fatalf("spec = %+v", api.spec)
	}
	if api.params.TemplateID != "tpl-1" || api.params.BusinessPhoneID != "phone-1" || api.params.Message == nil || api.params.Document == nil || api.params.Subject != "Oi" || api.params.Automation != (campaign.Automation{WorkflowID: "wf-1", AgentID: "agent-1"}) {
		t.Fatalf("params = %+v", api.params)
	}
	if !strings.Contains(rec.Body.String(), `"sendable":3`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/count", []byte(`{`), "application/json"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), codeBodyInvalid) {
		t.Fatalf("broken body = %d %s", rec.Code, rec.Body)
	}
	api.err = campaignaudience.ErrListExpired
	if rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/count", []byte(`{}`), "application/json"); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "audience_list_expired") {
		t.Fatalf("expired = %d %s", rec.Code, rec.Body)
	}
}

func TestUploadTakesAMultipartFileWithItsContextOrPastedText(t *testing.T) {
	api := &fakeAPI{}
	router, _ := server(t, api)
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, _ := form.CreateFormFile("file", "lista.xlsx")
	_, _ = part.Write([]byte("PK\x03\x04data"))
	_ = form.WriteField("context", `{"templateId":"tpl-1","variables":[{"key":"1","source":"lead.district"}]}`)
	_ = form.Close()
	rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/lists", buf.Bytes(), form.FormDataContentType())
	if rec.Code != http.StatusCreated || api.upload.FileName != "lista.xlsx" || string(api.upload.Data) != "PK\x03\x04data" || api.view.Params.TemplateID != "tpl-1" || api.view.Variables[0].Source != "lead.district" {
		t.Fatalf("multipart = %d %+v %+v", rec.Code, api.upload, api.view)
	}
	rec = call(router, http.MethodPost, "/campaign-audience/unofficial/lists", []byte(`{"fileName":"colado.csv","text":"numero\n5511900010142"}`), "application/json")
	if rec.Code != http.StatusCreated || api.upload.Empty || string(api.upload.Data) != "numero\n5511900010142" || api.channel != campaignaudience.ChannelUnofficial {
		t.Fatalf("text = %d %+v", rec.Code, api.upload)
	}
	rec = call(router, http.MethodPost, "/campaign-audience/whatsapp/lists", []byte(`{}`), "application/json")
	if rec.Code != http.StatusCreated || !api.upload.Empty {
		t.Fatalf("empty = %d %+v", rec.Code, api.upload)
	}
	var missing bytes.Buffer
	noFile := multipart.NewWriter(&missing)
	_ = noFile.WriteField("context", `{}`)
	_ = noFile.Close()
	if rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/lists", missing.Bytes(), noFile.FormDataContentType()); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "audience_file_required") {
		t.Fatalf("no file = %d %s", rec.Code, rec.Body)
	}
}

func TestRowsAreSearchedAndEditedByPath(t *testing.T) {
	api := &fakeAPI{}
	router, _ := server(t, api)
	rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/lists/list-1/rows/search", []byte(`{"search":"ana","missingOnly":true,"page":2,"limit":1,"templateId":"tpl-1"}`), "application/json")
	if rec.Code != http.StatusOK || api.ids[0] != "list-1" || api.query.Search != "ana" || !api.query.MissingOnly || api.query.Page != 2 || api.query.View.Params.TemplateID != "tpl-1" {
		t.Fatalf("search = %d %+v", rec.Code, api.query)
	}
	var page struct {
		Meta struct {
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Meta.TotalItems != 3 || page.Meta.TotalPages != 3 {
		t.Fatalf("meta = %s", rec.Body)
	}
	if rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/lists/list-1/rows", []byte(`{"contact":"11900010142","variables":["a"]}`), "application/json"); rec.Code != http.StatusCreated || api.row.Contact != "11900010142" {
		t.Fatalf("add = %d %+v", rec.Code, api.row)
	}
	if rec := call(router, http.MethodPatch, "/campaign-audience/whatsapp/lists/list-1/rows/r9", []byte(`{"contact":"11900010143"}`), "application/json"); rec.Code != http.StatusOK || !slices.Equal(api.ids, []string{"list-1", "r9"}) {
		t.Fatalf("edit = %d %v", rec.Code, api.ids)
	}
	if rec := call(router, http.MethodDelete, "/campaign-audience/whatsapp/lists/list-1/rows/r9", nil, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"r9"`) {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	api.err = campaignaudience_usecase.ErrPageInvalid
	if rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/lists/list-1/rows/search", []byte(`{"page":0}`), "application/json"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodePageInvalid) {
		t.Fatalf("bad page = %d %s", rec.Code, rec.Body)
	}
}

func TestTheSampleIsADownload(t *testing.T) {
	api := &fakeAPI{sample: campaignaudience_usecase.File{Name: "modelo-publico.csv", ContentType: "text/csv; charset=utf-8", Data: []byte("number;name\n")}}
	router, _ := server(t, api)
	rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/sample", []byte(`{"templateId":"tpl-1"}`), "application/json")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Disposition") != `attachment; filename="modelo-publico.csv"` || rec.Body.String() != "number;name\n" || api.params.TemplateID != "tpl-1" {
		t.Fatalf("sample = %d %v %q", rec.Code, rec.Header(), rec.Body)
	}
	api.err = campaignaudience_usecase.ErrSampleFormat
	if rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/sample", []byte(`{"format":"pdf"}`), "application/json"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodeSampleFormat) {
		t.Fatalf("format = %d %s", rec.Code, rec.Body)
	}
}

func TestSuggestionsAreGuardedByTemplatesAndAMissingTemplateIs404(t *testing.T) {
	api := &fakeAPI{}
	router, guards := server(t, api)
	rec := call(router, http.MethodPut, "/whatsapp/templates/tpl-1/variable-suggestions", []byte(`{"suggestions":[{"slot":1,"source":"lead.first_name"}]}`), "application/json")
	if rec.Code != http.StatusOK || len(api.saved) != 1 || (*guards)[0] != (guard{workspace.ResourceWhatsAppTemplates, workspace.ActionUpdate}) {
		t.Fatalf("save = %d %+v %+v", rec.Code, api.saved, *guards)
	}
	api.err = template.ErrTemplateAccessDenied
	if rec := call(router, http.MethodGet, "/whatsapp/templates/tpl-2/variable-suggestions", nil, ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), codeTemplate) {
		t.Fatalf("missing = %d %s", rec.Code, rec.Body)
	}
}

func TestTheCodesTheFrontTranslatesStayInStep(t *testing.T) {
	got := Codes()
	sort.Strings(got)
	got = slices.Compact(got)
	want := []string{
		"audience_busy", "audience_channel_invalid", "audience_contact_column_missing", "audience_file_empty", "audience_file_required",
		"audience_file_too_large", "audience_file_too_wide", "audience_file_unreadable", "audience_kind_required", "audience_lead_status_invalid", "audience_leads_metadata_unavailable", "audience_list_channel_mismatch",
		"audience_list_expired", "audience_list_metadata_missing", "audience_list_not_found", "audience_list_required", "audience_list_too_large", "audience_list_variables_missing",
		"audience_message_required", "audience_missing_policy_invalid", "audience_page_invalid", "audience_request_invalid", "audience_row_contact_invalid",
		"audience_row_duplicate", "audience_row_not_found", "audience_sample_format_invalid", "audience_selection_required", "audience_source_ambiguous",
		"audience_source_invalid", "audience_source_unsupported", "audience_template_required", "audience_text_required", "audience_unavailable",
		"audience_variable_choice_required", "audience_variable_duplicate", "audience_variable_source_required", "audience_variable_unknown",
		"campaign_agent_not_found", "campaign_automation_unavailable", "campaign_lead_targets_unavailable", "campaign_workflow_forbidden", "campaign_workflow_not_found", "email_design_malformed", "forbidden", "invalid_filter",
		"selection_ambiguous", "selection_changed", "selection_count_required", "selection_empty", "selection_everyone_unconfirmed",
		"selection_filter_required", "selection_fingerprint_mismatch", "selection_invalid_count", "selection_invalid_ids", "selection_invalid_page",
		"selection_limit_required", "selection_mode_unsupported", "selection_scope_denied", "selection_too_many_ids", "selection_unavailable",
		"selection_unknown_mode", "selection_unknown_sort",
		"send_binding_field_unknown", "send_binding_literal_empty", "send_binding_sensitive", "send_binding_unknown", "send_bindings_mismatch",
		"send_creation_scope_missing", "send_instance_unavailable",
		"send_template_unavailable", "template_not_found", "template_suggestion_duplicate", "template_suggestion_literal", "template_suggestion_slot_invalid",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("codes changed; update the front catalog campaignAudience.errors and this list:\n got %q\nwant %q", got, want)
	}
}

func TestTheAutomationOfTheCountIsRefusedWithItsCampaignCode(t *testing.T) {
	api := &fakeAPI{}
	router, _ := server(t, api)
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{err: campaign.ErrWorkflowNotFound, status: http.StatusUnprocessableEntity, code: "campaign_workflow_not_found"},
		{err: campaign.ErrWorkflowForbidden, status: http.StatusForbidden, code: "campaign_workflow_forbidden"},
		{err: campaign.ErrAgentNotFound, status: http.StatusUnprocessableEntity, code: "campaign_agent_not_found"},
		{err: campaign.ErrAutomationUnavailable, status: http.StatusServiceUnavailable, code: "campaign_automation_unavailable"},
		{err: campaignaudience.ErrListMetadataMissing, status: http.StatusUnprocessableEntity, code: "audience_list_metadata_missing"},
		{err: campaignaudience.ErrLeadsMetadataUnavailable, status: http.StatusUnprocessableEntity, code: "audience_leads_metadata_unavailable"},
	}
	for _, tc := range cases {
		api.err = tc.err
		rec := call(router, http.MethodPost, "/campaign-audience/whatsapp/count", []byte(`{"workflowId":"wf-1"}`), "application/json")
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
			t.Fatalf("%v = %d %s", tc.err, rec.Code, rec.Body)
		}
	}
}

func TestTheAudienceLeadsAreFilteredAndPagedLikeTheCount(t *testing.T) {
	api := &fakeAPI{}
	router, guards := server(t, api)
	body := `{"source":"leads","selection":{"mode":"ids","ids":["lead-1","lead-2"]},"subject":"Oi","status":"out","reason":"no_email","variable":"first_name","metadataKey":"turma","search":"ana","page":2,"limit":1}`
	rec := call(router, http.MethodPost, "/campaign-audience/email/leads", []byte(body), "application/json")
	if rec.Code != http.StatusOK || (*guards)[0] != (guard{workspace.ResourceEmailMarketing, workspace.ActionCreate}) {
		t.Fatalf("leads = %d %s %+v", rec.Code, rec.Body, *guards)
	}
	want := campaignaudience.LeadFilter{Status: campaignaudience.LeadOut, Reason: "no_email", Variable: "first_name", MetadataKey: "turma", Search: "ana"}
	if api.leads.Filter != want || api.leads.Page != 2 || api.leads.Limit != 1 || api.params.Subject != "Oi" || api.spec.Selection == nil || len(api.spec.Selection.IDs) != 2 {
		t.Fatalf("query = %+v params %+v spec %+v", api.leads, api.params, api.spec)
	}
	var page struct {
		Data []campaignaudience.AudienceLead `json:"data"`
		Meta struct {
			TotalItems int `json:"totalItems"`
			TotalPages int `json:"totalPages"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Data) != 1 || page.Data[0].Reason != "no_email" || page.Meta.TotalItems != 3 || page.Meta.TotalPages != 3 {
		t.Fatalf("body = %s", rec.Body)
	}
	api.err = campaignaudience.ErrLeadStatusInvalid
	if rec := call(router, http.MethodPost, "/campaign-audience/email/leads", []byte(`{"status":"sent","page":1,"limit":1}`), "application/json"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "audience_lead_status_invalid") {
		t.Fatalf("bad status = %d %s", rec.Code, rec.Body)
	}
	api.err = campaignaudience_usecase.ErrPageInvalid
	if rec := call(router, http.MethodPost, "/campaign-audience/email/leads", []byte(`{"page":0}`), "application/json"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), CodePageInvalid) {
		t.Fatalf("bad page = %d %s", rec.Code, rec.Body)
	}
}
