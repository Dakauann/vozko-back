package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

type recordedNodeTest struct {
	workflow_usecase.TestNodeUseCase
	input  *workflow_usecase.TestNodeInput
	viewer shared.Person
	err    error
}

func (r *recordedNodeTest) Execute(ctx context.Context, input workflow_usecase.TestNodeInput) (*workflow_usecase.TestNodeOutput, error) {
	r.input = &input
	r.viewer, _ = httpx.PersonFromContext(ctx)
	if r.err != nil {
		return nil, r.err
	}
	return &workflow_usecase.TestNodeOutput{NodeID: input.NodeID, Success: true}, nil
}

func nodeTestRequest(body string) *http.Request {
	r := workflowRequestAs(http.MethodPost, "/workflows/w1/nodes/n7/test", []byte(body), &auth.Claims{UserID: "u1"})
	return mux.SetURLVars(r, map[string]string{"id": "w1", "nodeId": "n7"})
}

func TestTheNodeTestRequestCarriesEveryDebugFieldToTheUseCase(t *testing.T) {
	uc := &recordedNodeTest{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, testNodeUC: uc}).TestNode(rec, nodeTestRequest(`{
		"mockedState": {"cep": "01001000"},
		"triggerVars": {"message": "oi"},
		"skipExecution": true,
		"confirmRealCalls": true,
		"continueWaits": true,
		"waitAnswers": {"w1": {"reply": "sim"}, "menu": {"optionId": "grad"}, "form": {"form": {"email": "ana@example.com"}}}
	}`))

	if rec.Code != http.StatusOK || uc.input == nil {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	in := *uc.input
	if in.WorkflowID != "w1" || in.NodeID != "n7" || in.WorkspaceID != "ws1" {
		t.Fatalf("ids = %+v", in)
	}
	if !in.ConfirmRealCalls || !in.ContinueWaits || !in.SkipExecution {
		t.Fatalf("flags = confirm %v continue %v skip %v", in.ConfirmRealCalls, in.ContinueWaits, in.SkipExecution)
	}
	want := map[string]workflow_usecase.TestWaitAnswer{
		"w1":   {Reply: "sim"},
		"menu": {OptionID: "grad"},
		"form": {Form: map[string]interface{}{"email": "ana@example.com"}},
	}
	if !reflect.DeepEqual(in.WaitAnswers, want) {
		t.Fatalf("waitAnswers = %+v", in.WaitAnswers)
	}
	if in.MockedState["cep"] != "01001000" || in.TriggerVars["message"] != "oi" {
		t.Fatalf("state = %v %v", in.MockedState, in.TriggerVars)
	}
	if uc.viewer.UserID != "u1" {
		t.Fatalf("the use case reads the caller from the request context, got %+v", uc.viewer)
	}
}

func TestANodeTestWithoutTheDebugFieldsConfirmsNothing(t *testing.T) {
	uc := &recordedNodeTest{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, testNodeUC: uc}).TestNode(rec, nodeTestRequest(`{}`))

	if rec.Code != http.StatusOK || uc.input == nil {
		t.Fatalf("status = %d", rec.Code)
	}
	if uc.input.ConfirmRealCalls || uc.input.ContinueWaits || uc.input.WaitAnswers != nil {
		t.Fatalf("input = %+v; a missing confirmation must never allow a real call", uc.input)
	}
}

func TestANodeTestWithAMalformedDebugFieldIsRefusedBeforeRunning(t *testing.T) {
	for name, body := range map[string]string{
		"confirmRealCalls as text": `{"confirmRealCalls": "sim"}`,
		"continueWaits as number":  `{"continueWaits": 1}`,
		"waitAnswers as a list":    `{"waitAnswers": ["sim"]}`,
		"a reply as a number":      `{"waitAnswers": {"w1": {"reply": 3}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			uc := &recordedNodeTest{}
			rec := httptest.NewRecorder()
			(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, testNodeUC: uc}).TestNode(rec, nodeTestRequest(body))
			if rec.Code != http.StatusBadRequest || uc.input != nil {
				t.Fatalf("status = %d, ran = %v", rec.Code, uc.input != nil)
			}
		})
	}
}

func TestANodeTestOfAnotherDepartmentsWorkflowIsRefusedBeforeRunning(t *testing.T) {
	uc := &recordedNodeTest{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{getErr: workflow.ErrWorkflowAccessDenied}, testNodeUC: uc}).TestNode(rec, nodeTestRequest(`{}`))
	if rec.Code != http.StatusForbidden || uc.input != nil {
		t.Fatalf("status = %d, ran = %v", rec.Code, uc.input != nil)
	}
}

func TestANodeTestOfAMissingWorkflowOrNodeAnswers404(t *testing.T) {
	for name, err := range map[string]error{
		"workflow": workflow.ErrWorkflowNotFound,
		"node":     fmt.Errorf("%w: n7", workflow.ErrNodeNotFound),
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, testNodeUC: &recordedNodeTest{err: err}}).TestNode(rec, nodeTestRequest(`{"confirmRealCalls": true}`))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d", rec.Code)
			}
		})
	}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, testNodeUC: &recordedNodeTest{err: errors.New("boom")}}).TestNode(rec, nodeTestRequest(`{}`))
	if rec.Code == http.StatusOK {
		t.Fatal("a failing node test must not answer 200")
	}
}
