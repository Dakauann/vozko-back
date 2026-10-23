package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response/responsetest"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

type lastInputStub struct {
	result *workflow_usecase.LastTriggerInput
	err    error
	viewer shared.Person
	wf     *workflow.Workflow
	nodeID string
}

func (s *lastInputStub) Execute(viewer shared.Person, wf *workflow.Workflow, nodeID string) (*workflow_usecase.LastTriggerInput, error) {
	s.viewer, s.wf, s.nodeID = viewer, wf, nodeID
	return s.result, s.err
}

func lastInputRequest() *http.Request {
	r := workflowRequestAs(http.MethodGet, "/workflows/w1/triggers/t1/last-input", nil, &auth.Claims{UserID: "u1"})
	return mux.SetURLVars(r, map[string]string{"id": "w1", "nodeId": "t1"})
}

func TestTheLastRealInputAnswersTheInputItsMaskedKeysAndTheRun(t *testing.T) {
	started := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	stub := &lastInputStub{result: &workflow_usecase.LastTriggerInput{Input: map[string]interface{}{"cpf": "•••"}, MaskedKeys: []string{"cpf"}, RunID: "run-7", StartedAt: started}}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, lastTriggerInput: stub}).LastTriggerInput(rec, lastInputRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.viewer.UserID != "u1" || stub.wf == nil || stub.wf.ID != "w1" || stub.nodeID != "t1" {
		t.Fatalf("stub got viewer %+v wf %+v node %q", stub.viewer, stub.wf, stub.nodeID)
	}
	var body struct {
		Input      map[string]interface{} `json:"input"`
		MaskedKeys []string               `json:"maskedKeys"`
		RunID      string                 `json:"runId"`
		StartedAt  time.Time              `json:"startedAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RunID != "run-7" || !body.StartedAt.Equal(started) || !reflect.DeepEqual(body.MaskedKeys, []string{"cpf"}) || body.Input["cpf"] != "•••" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestTheLastRealInputRefusalsAreCoded(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{workflow.ErrNoRecentTriggerInput, http.StatusNotFound, workflow_usecase.NoRecentTriggerInputCode},
		{workflow.ErrTriggerInputNotFound, http.StatusNotFound, workflow_usecase.TriggerNotFoundCode},
		{workflow.ErrTriggerInputUnavailable, http.StatusServiceUnavailable, workflow_usecase.TriggerInputUnavailableCode},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, lastTriggerInput: &lastInputStub{err: tc.err}}).LastTriggerInput(rec, lastInputRequest())
		if rec.Code != tc.status || responsetest.CodeOf(t, rec) != tc.code {
			t.Fatalf("%v: status %d body %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}

func TestTheNodeTestCarriesTheChosenTriggerAndAnswersItsIssues(t *testing.T) {
	uc := &recordedNodeTest{err: &workflow.TriggerInputInvalidError{Issues: []workflow.TriggerInputIssue{{Code: workflow.IssueRequired, Key: "cpf"}}}}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, testNodeUC: uc}).TestNode(rec, nodeTestRequest(`{"trigger": {"nodeId": "agente", "input": {"curso": "ADS"}}}`))

	if uc.input == nil || uc.input.Trigger == nil || uc.input.Trigger.NodeID != "agente" || uc.input.Trigger.Input["curso"] != "ADS" {
		t.Fatalf("input = %+v", uc.input)
	}
	if rec.Code != http.StatusUnprocessableEntity || responsetest.CodeOf(t, rec) != workflow_usecase.TriggerInputInvalidCode {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Details struct {
			Issues []workflow.TriggerInputIssue `json:"issues"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body.Details.Issues, []workflow.TriggerInputIssue{{Code: workflow.IssueRequired, Key: "cpf"}}) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestASavedTestInputErrorIsACodedValidationError(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WorkflowHandler{}).handleDomainError(rec, workflow.ErrTestInputTooLarge)
	if rec.Code != http.StatusUnprocessableEntity || responsetest.CodeOf(t, rec) != "test_input_invalid" {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
