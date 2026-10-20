package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
	"vozko/pkg/codegolden"
)

var workflowErrorCodesGolden = filepath.Join("testdata", "workflow_error_codes.golden")

const workflowErrorCodesAdvice = "update testdata/workflow_error_codes.golden, and also add the code to vozko-front src/lib/workflows/error-codes.ts and translate workflowsPage.errors.<code> in the pt, en, es and de catalogs"

func TestEveryWorkflowDomainErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, workflowDomainRefusals, (&WorkflowHandler{}).handleDomainError)
}

func TestEveryWorkflowValidationErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, workflowValidationRefusals, (&WorkflowHandler{}).handleDomainError)
}

func TestEveryWorkflowRequestRefusalHasAStableCode(t *testing.T) {
	responsetest.RequireStableCodes(t, workflowRequestRefusals)
}

func TestAnUnmappedWorkflowErrorStillWritesACode(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WorkflowHandler{}).handleDomainError(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError || responsetest.CodeOf(t, rec) != refusalWorkflowInternal.Code {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowErrorCodesMatchTheGoldenSoTheFrontTranslatesThem(t *testing.T) {
	codes := response.RefusalCodes(
		response.ErrorRefusalsOf(workflowDomainRefusals),
		response.ErrorRefusalsOf(workflowValidationRefusals),
		workflowRequestRefusals,
	)
	codegolden.Compare(t, workflowErrorCodesGolden, codes, workflowErrorCodesAdvice)
}
