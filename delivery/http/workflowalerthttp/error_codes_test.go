package workflowalerthttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/pkg/codegolden"
)

const alertErrorCodesAdvice = "update testdata/error_codes.golden, and also add the code to vozko-front src/lib/workflows/error-codes.ts and translate workflowsPage.errors.<code> in the pt, en, es and de catalogs"

func TestEveryAlertRefusalHasAStableCode(t *testing.T) {
	responsetest.RequireStableCodes(t, alertRefusals)
}

func TestUnreadableAlertListsAreRefusedWithTheirCode(t *testing.T) {
	failing := NewHandler(&stubAlerts{err: errors.New("db down")}, &stubAccess{})
	tests := []struct {
		name string
		list func(http.ResponseWriter, *http.Request)
		path string
		vars map[string]string
		want string
	}{
		{"a workflow's alerts", failing.ListForWorkflow, "/workflows/wf-1/alerts", map[string]string{"id": "wf-1"}, refusalWorkflowAlertsFailed.Code},
		{"the workspace alerts", failing.ListOpen, "/workflow-alerts", nil, refusalWorkspaceAlertsFailed.Code},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.list(rec, request(tc.path, tc.vars))
			if rec.Code != http.StatusInternalServerError || responsetest.CodeOf(t, rec) != tc.want {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAlertErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := response.RefusalCodes(alertRefusals, response.ErrorRefusalsOf(httpx.WorkflowAccessRefusals), []response.Refusal{httpx.RefusalWorkflowUnreadable})
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, alertErrorCodesAdvice)
}
