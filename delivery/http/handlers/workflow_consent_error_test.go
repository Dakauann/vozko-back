package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/workflow"
)

func TestAConsentGrantWithoutAReplyIsRefusedWithItsCode(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WorkflowHandler{}).handleDomainError(rec, fmt.Errorf("%w: node %q", workflow.ErrConsentGrantNeedsReply, "c"))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "workflow_consent_grant_needs_reply" {
		t.Fatalf("code = %q", body.Code)
	}
}
