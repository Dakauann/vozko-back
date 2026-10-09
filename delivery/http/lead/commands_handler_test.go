package lead

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

	ca "vozko/domain/audience"
	"vozko/domain/auth"
	"vozko/domain/conversation"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	wc_entry "vozko/domain/whatsapp_campaign_entry"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	lead_usecase "vozko/usecases/lead"
)

const routeLeadID = "6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01"

type stubCommands struct {
	expected   *int64
	err        error
	result     *leaddomain.Lead
	calls      []string
	draft      leaddomain.Draft
	edit       leaddomain.Edit
	relative   lead_usecase.AddRelativeInput
	linked     [2]string
	kind       leaddomain.RelationKind
	removed    string
	area       leaddomain.Area
	duplicates []lead_usecase.DuplicateWarning
	optOut     leaddomain.OptOutSource
}

func (s *stubCommands) answer(call string, expected *int64) (*leaddomain.Lead, error) {
	s.calls = append(s.calls, call)
	s.expected = expected
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubCommands) Create(_ context.Context, _ lead_usecase.Actor, d leaddomain.Draft) (lead_usecase.CreateResult, error) {
	s.draft = d
	l, err := s.answer("create", nil)
	return lead_usecase.CreateResult{Lead: l, Duplicates: s.duplicates}, err
}

func (s *stubCommands) Update(_ context.Context, _ lead_usecase.Actor, _ string, expected *int64, e leaddomain.Edit) (*leaddomain.Lead, error) {
	s.edit = e
	return s.answer("update", expected)
}

func (s *stubCommands) Rename(_ context.Context, _ lead_usecase.Actor, _ string, expected *int64, _ string) (*leaddomain.Lead, error) {
	return s.answer("rename", expected)
}

func (s *stubCommands) Block(_ context.Context, _ lead_usecase.Actor, _ string, _ lead_usecase.BlockInput) (lead_usecase.BlockResult, error) {
	l, err := s.answer("block", nil)
	return lead_usecase.BlockResult{Lead: l}, err
}

func (s *stubCommands) SetOwner(_ context.Context, _ lead_usecase.Actor, _, _ string) (*leaddomain.Lead, error) {
	return s.answer("owner", nil)
}

func (s *stubCommands) OptOut(_ context.Context, _ lead_usecase.Actor, _ string, source leaddomain.OptOutSource) (*leaddomain.Lead, error) {
	s.optOut = source
	return s.answer("opt-out", nil)
}

type stubHistory struct {
	entries        []wc_entry.WhatsAppCampaignEntry
	err            error
	reads          []string
	detail         *lead_usecase.LeadDetail
	relatives      leaddomain.RelativesPage
	relativesQuery leaddomain.RelativesQuery
	card           leaddomain.Card
	cardEntry      string
}

func (s *stubHistory) Detail(_ context.Context, _ conversation.Viewer, leadID string) (lead_usecase.LeadDetail, error) {
	s.reads = append(s.reads, "detail:"+leadID)
	if s.err != nil {
		return lead_usecase.LeadDetail{}, s.err
	}
	if s.detail != nil {
		return *s.detail, nil
	}
	return lead_usecase.LeadDetail{
		Lead:      &leaddomain.Lead{ID: leadID, Number: "5511987654321", Version: 7},
		Campaigns: []lead_usecase.CampaignHistory{{CampaignID: "c-1", CampaignName: "Boas-vindas", Entries: s.entries}},
		Summary:   leaddomain.LeadSummary{WhatsAppCampaigns: len(s.entries), TotalCampaigns: len(s.entries), WhatsAppWindowOpen: true},
	}, nil
}

func (s *stubHistory) EntriesInCampaign(_ conversation.Viewer, _, _ string, _ shared.EntryType) ([]wc_entry.WhatsAppCampaignEntry, error) {
	return s.entries, s.err
}

func (s *stubHistory) Analyses(_ context.Context, _ conversation.Viewer, leadID, _ string, _ shared.EntryType) (*leaddomain.Lead, []*ca.Analysis, error) {
	return &leaddomain.Lead{ID: leadID}, nil, s.err
}

func (s *stubHistory) EntryConversation(_ conversation.Viewer, entryID string, _ shared.EntryType) (lead_usecase.EntryConversation, error) {
	if s.err != nil {
		return lead_usecase.EntryConversation{}, s.err
	}
	return lead_usecase.EntryConversation{EntryID: entryID}, nil
}

func passThrough(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
	return next
}

func routedHandler(cmds *stubCommands, history *stubHistory) http.Handler {
	h := NewLeadHandler(HandlerDeps{Commands: cmds, History: history})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	RegisterEntryConversationRoutes(router, h, passThrough)
	return router
}

func send(t *testing.T, handler http.Handler, method, path string, headers map[string]string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "user"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestRecordEditsRequireIfMatch(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
			rec := send(t, routedHandler(cmds, &stubHistory{}), method, "/leads/"+routeLeadID, nil, map[string]any{"name": "Ana"})
			if rec.Code != http.StatusPreconditionRequired {
				t.Fatalf("status = %d, want 428", rec.Code)
			}
			if len(cmds.calls) != 0 {
				t.Fatal("the command ran without a version")
			}
		})
	}
}

func TestRecordEditsPassTheVersionFromIfMatch(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Name: "Ana", Version: 4}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPatch, "/leads/"+routeLeadID, map[string]string{"If-Match": `"3"`}, map[string]any{"name": "Ana"})
	if rec.Code != http.StatusOK || cmds.expected == nil || *cmds.expected != 3 {
		t.Fatalf("status = %d, expected = %v", rec.Code, cmds.expected)
	}
	var out LeadRecordResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Version != 4 || out.Name != "Ana" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAStaleEditAnswersConflictWithTheCurrentRecord(t *testing.T) {
	cmds := &stubCommands{err: &leaddomain.VersionConflict{Current: &leaddomain.Lead{ID: routeLeadID, Name: "Ana Souza", Version: 5}}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{"nickname": "Aninha"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var out LeadVersionConflictResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Code != "version_conflict" || out.Current.Version != 5 || out.Current.Name != "Ana Souza" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCommandErrorsMapToStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{leaddomain.ErrLeadOwnerOutOfReach, http.StatusForbidden, "lead_owner_out_of_reach"},
		{leaddomain.ErrLeadNotFound, http.StatusNotFound, "lead_not_found"},
		{leaddomain.ErrLeadDuplicate, http.StatusConflict, "lead_identity_taken"},
		{leaddomain.ErrLeadEmailInvalid, http.StatusBadRequest, "lead_email_invalid"},
		{leaddomain.ErrLeadOwnerOutsideWorkspace, http.StatusBadRequest, "lead_owner_outside_workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rec := send(t, routedHandler(&stubCommands{err: tc.err}, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/owner", nil, map[string]any{"ownerId": "x"})
			var out struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if rec.Code != tc.status || out.Code != tc.code {
				t.Fatalf("status = %d code = %q, want %d %q", rec.Code, out.Code, tc.status, tc.code)
			}
		})
	}
}

func TestCreateAnswersCreated(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Name: "Maria", Version: 1}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads", nil, map[string]any{"name": "Maria"})
	if rec.Code != http.StatusCreated || len(cmds.calls) != 1 || cmds.calls[0] != "create" {
		t.Fatalf("status = %d calls = %v", rec.Code, cmds.calls)
	}
}

func TestTargetedCommandsNeedNoVersion(t *testing.T) {
	for path, call := range map[string]string{"/block": "block", "/owner": "owner", "/opt-out": "opt-out"} {
		t.Run(call, func(t *testing.T) {
			bodies := map[string]any{"/block": map[string]any{"blocked": true}, "/owner": map[string]any{"ownerId": ""}, "/opt-out": nil}
			cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 2}}
			rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+path, nil, bodies[path])
			if rec.Code != http.StatusOK || len(cmds.calls) != 1 || cmds.calls[0] != call {
				t.Fatalf("status = %d calls = %v body %s", rec.Code, cmds.calls, rec.Body.String())
			}
		})
	}
}

func TestOptOutRecordsWhoAskedForIt(t *testing.T) {
	opted := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		body any
		sent leaddomain.OptOutSource
		want leaddomain.OptOutSource
	}{
		{name: "no body leaves the source to the use case", body: nil, sent: "", want: leaddomain.OptOutOperator},
		{name: "an empty object leaves the source to the use case", body: map[string]any{}, sent: "", want: leaddomain.OptOutOperator},
		{name: "the team decided", body: map[string]any{"source": "operator"}, sent: leaddomain.OptOutOperator, want: leaddomain.OptOutOperator},
		{name: "the lead asked", body: map[string]any{"source": "lead_request"}, sent: leaddomain.OptOutLeadRequest, want: leaddomain.OptOutLeadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 2, OptedOutAt: &opted, OptOutSource: tc.want}}
			rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/opt-out", nil, tc.body)
			if rec.Code != http.StatusOK || cmds.optOut != tc.sent {
				t.Fatalf("status = %d source = %q, want 200 and %q passed through (%s)", rec.Code, cmds.optOut, tc.sent, rec.Body.String())
			}
			var out LeadRecordResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.OptOutSource != string(tc.want) || out.OptedOutAt == nil {
				t.Fatalf("answer = %s, want the opt-out with its source", rec.Body.String())
			}
		})
	}
}

func TestOptOutRefusesAnUnknownKeyAndAnUnknownSource(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 2}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/opt-out", nil, map[string]any{"reason": "spam"})
	if rec.Code != http.StatusBadRequest || len(cmds.calls) != 0 {
		t.Fatalf("an unknown key: status = %d calls = %v, want 400 and no command", rec.Code, cmds.calls)
	}
	refusing := &stubCommands{err: leaddomain.ErrLeadOptOutSourceInvalid}
	rec = send(t, routedHandler(refusing, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/opt-out", nil, map[string]any{"source": "rumour"})
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadRequest || body.Code != "lead_opt_out_source_invalid" || refusing.optOut != "rumour" {
		t.Fatalf("an unknown source: status = %d body = %s, want 400 lead_opt_out_source_invalid", rec.Code, rec.Body.String())
	}
}

func TestStaticLeadRoutesAreNotTakenForAnIDAndIDsMustBeUUIDs(t *testing.T) {
	history := &stubHistory{}
	handler := routedHandler(&stubCommands{}, history)
	if rec := send(t, handler, http.MethodGet, "/leads/not-a-uuid", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a non uuid id must not reach a handler, status = %d", rec.Code)
	}
	if rec := send(t, handler, http.MethodGet, "/leads/"+routeLeadID, nil, nil); rec.Code != http.StatusOK || len(history.reads) != 1 {
		t.Fatalf("status = %d reads = %v", rec.Code, history.reads)
	}
	var out LeadDetailResponse
	rec := send(t, handler, http.MethodGet, "/leads/"+routeLeadID, nil, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Version != 7 || out.Number != "5511987654321" {
		t.Fatalf("detail = %s", rec.Body.String())
	}
}

func TestADetailOfAnotherWorkspaceIsNotFound(t *testing.T) {
	rec := send(t, routedHandler(&stubCommands{}, &stubHistory{err: leaddomain.ErrLeadNotFound}), http.MethodGet, "/leads/"+routeLeadID+"/campaigns/c-1/entries", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAConversationOutsideTheViewerScopeIsNotFound(t *testing.T) {
	rec := send(t, routedHandler(&stubCommands{}, &stubHistory{err: lead_usecase.ErrConversationNotVisible}), http.MethodGet, "/entries/e-1/conversation", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestWithoutCommandsTheWritesRefuse(t *testing.T) {
	h := NewLeadHandler(HandlerDeps{History: &stubHistory{}})
	router := mux.NewRouter()
	RegisterRoutes(router, h, passThrough)
	rec := send(t, router, http.MethodPost, "/leads", nil, map[string]any{"name": "Maria"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestABlockWithoutTheBlockedFlagIsRefusedInsteadOfUnblocking(t *testing.T) {
	for name, body := range map[string]any{
		"empty":          map[string]any{},
		"only the phone": map[string]any{"businessPhoneId": "bp-1"},
		"a typo":         map[string]any{"block": true},
	} {
		t.Run(name, func(t *testing.T) {
			cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Blocked: true, Version: 2}}
			rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/block", nil, body)
			if rec.Code != http.StatusBadRequest || len(cmds.calls) != 0 {
				t.Fatalf("status = %d calls = %v, want 400 and no command", rec.Code, cmds.calls)
			}
		})
	}
}

func TestCommandRoutesRefuseFieldsTheyDoNotTake(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		body    map[string]any
	}{
		{"the owner on an edit", http.MethodPut, "/leads/" + routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{"ownerId": "u-2"}},
		{"a typo on a rename", http.MethodPatch, "/leads/" + routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{"nmae": "Ana"}},
		{"an unknown key on a create", http.MethodPost, "/leads", nil, map[string]any{"name": "Ana", "ownerId": "u-2"}},
		{"an unknown key on the owner", http.MethodPost, "/leads/" + routeLeadID + "/owner", nil, map[string]any{"ownerId": "", "kind": "ai"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
			rec := send(t, routedHandler(cmds, &stubHistory{}), tc.method, tc.path, tc.headers, tc.body)
			var out struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if rec.Code != http.StatusBadRequest || out.Code != "invalid_body" || len(cmds.calls) != 0 {
				t.Fatalf("status = %d code = %q calls = %v", rec.Code, out.Code, cmds.calls)
			}
		})
	}
}

func TestTheDetailCarriesTheCampaignsAndTheWindowFromTheHistory(t *testing.T) {
	history := &stubHistory{entries: []wc_entry.WhatsAppCampaignEntry{{ID: "e-1", CampaignID: "c-1"}}}
	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/leads/"+routeLeadID, nil, nil)
	var out LeadDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(out.Campaigns) != 1 || out.Campaigns[0].CampaignName != "Boas-vindas" || len(out.Campaigns[0].Entries) != 1 ||
		out.WhatsAppCampaigns != 1 || !out.WhatsAppWindowOpen {
		t.Fatalf("detail = %+v", out)
	}
}

func TestAnUnreadableDetailIsAServerError(t *testing.T) {
	rec := send(t, routedHandler(&stubCommands{}, &stubHistory{err: errors.New("database down")}), http.MethodGet, "/leads/"+routeLeadID, nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
