package choicestatshttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/domain/auth"
	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
	"vozko/pkg/codegolden"
)

type fakeReader struct {
	err          error
	scope        workflow.Scope
	version      *int
	from, to     string
	access       wc.ReportAccess
	unofficialWS string
}

func (f *fakeReader) Workflow(_ context.Context, scope workflow.Scope, _ string, from, to string, version *int) (*choicestats.WorkflowReport, error) {
	f.scope, f.version, f.from, f.to = scope, version, from, to
	if f.err != nil {
		return nil, f.err
	}
	return &choicestats.WorkflowReport{From: from, To: to, Timezone: "America/Sao_Paulo", Version: version, Nodes: []choicestats.NodeStats{}}, nil
}

func (f *fakeReader) WhatsAppCampaign(_ context.Context, access wc.ReportAccess, _ string, from, to string) (*choicestats.PromptReport, error) {
	f.access, f.from, f.to = access, from, to
	return &choicestats.PromptReport{Timezone: "America/Sao_Paulo", Prompt: choicestats.Summarize(nil, nil)}, f.err
}

func (f *fakeReader) UnofficialCampaign(_ context.Context, workspaceID string, _ uw.DepartmentScope, _ string, from, to string) (*choicestats.PromptReport, error) {
	f.unofficialWS = workspaceID
	return &choicestats.PromptReport{Prompt: choicestats.Summarize(nil, nil)}, f.err
}

func (f *fakeReader) Template(_ context.Context, _ string, _ string, from, to string) (*choicestats.PromptReport, error) {
	return &choicestats.PromptReport{Prompt: choicestats.Summarize(nil, nil)}, f.err
}

type allowAll struct{}

func (allowAll) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

func request(method, target string, vars map[string]string, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	ctx = context.WithValue(ctx, middleware.DepartmentFilterContextKey, &dept.DepartmentFilter{})
	return mux.SetURLVars(r.WithContext(ctx), vars)
}

func TestTheWorkflowReadPassesThePeriodVersionAndScope(t *testing.T) {
	reader := &fakeReader{}
	h := NewHandler(reader, allowAll{})
	rec := httptest.NewRecorder()
	h.Workflow(rec, request(http.MethodGet, "/workflows/wf-1/choice-stats?from=2026-10-01&to=2026-10-21&version=3", map[string]string{"id": "wf-1"}, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if reader.scope.WorkspaceID != "ws-1" || reader.version == nil || *reader.version != 3 || reader.from != "2026-10-01" || reader.to != "2026-10-21" {
		t.Fatalf("reader got %+v", reader)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"from", "to", "timezone", "version", "nodes"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("body misses %q: %s", key, rec.Body.String())
		}
	}

	rec = httptest.NewRecorder()
	h.Workflow(rec, request(http.MethodGet, "/workflows/wf-1/choice-stats?from=2026-10-01&to=2026-10-21&version=abc", map[string]string{"id": "wf-1"}, ""))
	if rec.Code != http.StatusBadRequest || responsetest.CodeOf(t, rec) != "invalid_version" {
		t.Fatalf("a bad version = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCampaignAndTemplateReadsAnswerThePromptShape(t *testing.T) {
	reader := &fakeReader{}
	h := NewHandler(reader, allowAll{})
	cases := []struct {
		name  string
		serve func(http.ResponseWriter, *http.Request)
	}{
		{"official", h.WhatsAppCampaign},
		{"unofficial", h.UnofficialCampaign},
		{"template", h.Template},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.serve(rec, request(http.MethodGet, "/x/c-1/choice-stats", map[string]string{"id": "c-1"}, ""))
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"prompt"`) || !strings.Contains(rec.Body.String(), `"options":[]`) {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
		})
	}
	if reader.access.WorkspaceID != "ws-1" || reader.access.AllowDepartment == nil || reader.unofficialWS != "ws-1" {
		t.Fatalf("access = %+v, unofficial workspace = %q", reader.access, reader.unofficialWS)
	}
}

func TestEveryChoiceStatsErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, readRefusals, writeReadError)
	responsetest.RequireStableCodes(t, requestRefusals)
}

func TestAnUnknownFailureIsUnavailableNeverZeros(t *testing.T) {
	h := NewHandler(&fakeReader{err: errors.New("boom")}, allowAll{})
	rec := httptest.NewRecorder()
	h.Template(rec, request(http.MethodGet, "/whatsapp-templates/t/choice-stats", map[string]string{"id": "t"}, ""))
	if rec.Code != http.StatusServiceUnavailable || responsetest.CodeOf(t, rec) != "choice_stats_unavailable" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestChoiceStatsErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := response.RefusalCodes(response.ErrorRefusalsOf(readRefusals), requestRefusals)
	codegolden.Compare(t, filepath.Join("testdata", "choice_stats_error_codes.golden"), codes,
		"update testdata/choice_stats_error_codes.golden, and translate choiceStats.errors.<code> in the pt, en, es and de catalogs of vozko-front")
}
