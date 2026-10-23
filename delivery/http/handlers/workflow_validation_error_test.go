package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/workflow"
)

type workflowErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details struct {
		Detail string               `json:"detail"`
		Issues []workflow.LintIssue `json:"issues"`
	} `json:"details"`
}

func workflowErrorAnswer(t *testing.T, err error) (int, workflowErrorBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	(&WorkflowHandler{}).handleDomainError(rec, err)
	var body workflowErrorBody
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &body); decodeErr != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), decodeErr)
	}
	return rec.Code, body
}

func TestDraftValidationErrorsAreRefusedAs422InPortugueseNotAs500(t *testing.T) {
	cases := []error{
		fmt.Errorf("%w: %q", workflow.ErrGraphTriggerNotConnected, "t"),
		fmt.Errorf("%w: node %q output %q", workflow.ErrNodeMissingRequiredOutput, "n", "true"),
		fmt.Errorf("%w: node %q label_id %q", workflow.ErrNodeInvalidLabelID, "n", "l"),
		fmt.Errorf("%w: node %q stage_id %q", workflow.ErrNodeInvalidStageID, "n", "s"),
		fmt.Errorf("%w: node %q pipeline_id %q", workflow.ErrNodeInvalidOpportunityPipeline, "n", "p"),
		fmt.Errorf("%w: node %q department_id %q", workflow.ErrNodeInvalidDepartmentID, "n", "d"),
		fmt.Errorf("%w: node %q member_id %q", workflow.ErrNodeInvalidMemberID, "n", "m"),
		fmt.Errorf("%w: node %q business_phone_id %q", workflow.ErrNodeInvalidBusinessPhoneID, "n", "b"),
		fmt.Errorf("%w: node %q workflow_id %q", workflow.ErrNodeInvalidWorkflowID, "n", "w"),
		fmt.Errorf("%w: node %q knowledge_base_id %q", workflow.ErrNodeInvalidKnowledgeBaseID, "n", "k"),
		fmt.Errorf("%w: node %q (action_send_text)", workflow.ErrNodeIncompatibleScope, "n"),
		fmt.Errorf("%w: node %q tool_mode %q", workflow.ErrNodeInvalidToolMode, "n", "x"),
		fmt.Errorf("%w: node %q response_mode %q", workflow.ErrNodeInvalidResponseMode, "n", "x"),
		fmt.Errorf("%w: AI agent node %q", workflow.ErrNodeSegmentedSendConflict, "n"),
		fmt.Errorf("%w: node %q: x", workflow.ErrNodeInvalidInteractiveConfig, "n"),
		fmt.Errorf("%w: node %q", workflow.ErrReplyToAgentWithoutTrigger, "n"),
		fmt.Errorf("%w: x", workflow.ErrAgentCallInputsInvalid),
		fmt.Errorf("%w: %q", workflow.ErrGraphTriggerHasIncoming, "t"),
		workflow.ErrInvalidStatus,
		workflow.ErrUnknownNodeType,
	}
	for _, err := range cases {
		status, body := workflowErrorAnswer(t, err)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("%v: status = %d, want 422", err, status)
			continue
		}
		if body.Message == "" || body.Message == "Erro interno do servidor" || strings.HasPrefix(body.Message, "workflow:") {
			t.Errorf("%v: message = %q, want Portuguese copy", err, body.Message)
		}
		if body.Details.Detail != err.Error() {
			t.Errorf("%v: details.detail = %q, want the error text", err, body.Details.Detail)
		}
	}
}

func TestKnownValidationMessagesAreKept(t *testing.T) {
	status, body := workflowErrorAnswer(t, workflow.ErrGraphNoEndNode)
	if status != http.StatusUnprocessableEntity || body.Message != "O workflow precisa ter pelo menos um nó de Fim" {
		t.Fatalf("status = %d message = %q", status, body.Message)
	}
	_, body = workflowErrorAnswer(t, fmt.Errorf("%w: node %q", workflow.ErrConsentGrantNeedsReply, "c"))
	if body.Code != workflowConsentGrantNeedsReplyCode {
		t.Fatalf("code = %q, the consent refusal keeps its own code", body.Code)
	}
}

func TestAnUnmappedValidationErrorGetsAGenericPortugueseMessage(t *testing.T) {
	status, body := workflowErrorAnswer(t, workflow.ErrUnknownNodeType)
	if status != http.StatusUnprocessableEntity || body.Message != workflowInvalidMessage || body.Code != workflowInvalidCode {
		t.Fatalf("status = %d code = %q message = %q", status, body.Code, body.Message)
	}
}

func TestOnlyATrulyUnknownErrorIsAServerError(t *testing.T) {
	status, body := workflowErrorAnswer(t, errors.New("connection reset"))
	if status != http.StatusInternalServerError || body.Message != "Erro interno do servidor" {
		t.Fatalf("status = %d message = %q", status, body.Message)
	}
}

func TestADraftThatCannotBePublishedListsEveryBlockingIssue(t *testing.T) {
	report := workflow.LintGraph(&workflow.Graph{Nodes: []workflow.Node{{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived, Config: map[string]interface{}{}}}}, workflow.WorkflowTypeMessages, nil, nil, nil)
	blocked := workflow.NotPublishableFrom(report, []error{fmt.Errorf("%w: node %q label_id %q", workflow.ErrNodeInvalidLabelID, "lbl", "gone")})

	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &publishingWorkflows{publishErr: blocked}}).Publish(rec, workflowRequestAs(http.MethodPost, "/workflows/w1/publish", nil, &auth.Claims{UserID: "u1"}))
	var body workflowErrorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if rec.Code != http.StatusUnprocessableEntity || body.Code != workflow.NotPublishableCode {
		t.Fatalf("status = %d code = %q body = %s", rec.Code, body.Code, rec.Body.String())
	}
	if len(body.Details.Issues) != len(report.Blocking())+1 {
		t.Fatalf("issues = %+v", body.Details.Issues)
	}
	last := body.Details.Issues[len(body.Details.Issues)-1]
	if last.Code != workflow.LintInvalidResourceRef || last.NodeID != "lbl" || last.Severity != workflow.SeverityBlocking || last.Message == "" {
		t.Fatalf("issue = %+v", last)
	}
	if !strings.Contains(body.Message, fmt.Sprint(len(body.Details.Issues))) {
		t.Fatalf("message = %q, want the problem count", body.Message)
	}
}
