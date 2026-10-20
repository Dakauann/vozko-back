package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/delivery/http/response"
	"vozko/delivery/http/response/responsetest"
)

func TestEveryWorkflowAccessErrorWritesItsStableCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, WorkflowAccessRefusals, WriteWorkflowAccessError)
	responsetest.RequireStableCodes(t, []response.Refusal{RefusalWorkflowUnreadable})
}

func TestAnUnreadableWorkflowIsRefusedWithACode(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteWorkflowAccessError(rec, errors.New("db down"))
	if rec.Code != http.StatusInternalServerError || responsetest.CodeOf(t, rec) != RefusalWorkflowUnreadable.Code {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
