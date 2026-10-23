package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response/responsetest"
	"vozko/domain/auth"
	"vozko/domain/workflow"
)

type variablesStub struct {
	catalog workflow.VariableCatalog
	err     error
	wf      *workflow.Workflow
	nodeID  string
}

func (s *variablesStub) At(_ context.Context, wf *workflow.Workflow, nodeID string) (workflow.VariableCatalog, error) {
	s.wf, s.nodeID = wf, nodeID
	return s.catalog, s.err
}

func variablesRequest() *http.Request {
	r := workflowRequestAs(http.MethodGet, "/workflows/w1/nodes/n1/variables", nil, &auth.Claims{UserID: "u1"})
	return mux.SetURLVars(r, map[string]string{"id": "w1", "nodeId": "n1"})
}

func TestTheNodeVariablesAnswerTheCatalogOfTheScopedWorkflow(t *testing.T) {
	stub := &variablesStub{catalog: workflow.VariableCatalog{Variables: []workflow.CatalogVariable{{
		Ref: "input.cpf", Label: "Enviado pelo agente › cpf", Group: workflow.VariableGroupTrigger, Kind: workflow.VariableKindTrigger,
		Source: &workflow.VariableSource{NodeID: "t", NodeName: "Bia chama matrícula"}, Type: workflow.VariableTypeText, Availability: workflow.AvailableAlways,
	}}}}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, variables: stub}).NodeVariables(rec, variablesRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.wf == nil || stub.wf.ID != "w1" || stub.nodeID != "n1" {
		t.Fatalf("stub got wf %+v node %q", stub.wf, stub.nodeID)
	}
	var body struct {
		Variables []map[string]interface{} `json:"variables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	v := body.Variables[0]
	source, _ := v["source"].(map[string]interface{})
	if v["ref"] != "input.cpf" || v["group"] != "trigger" || v["kind"] != "trigger" || v["type"] != "text" || v["availability"] != "always" ||
		source["nodeId"] != "t" || source["nodeName"] != "Bia chama matrícula" || v["example"] != nil {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if _, present := v["example"]; !present {
		t.Fatalf("example must be present as null, body = %s", rec.Body.String())
	}
}

func TestTheNodeVariablesRefusalsAreCoded(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{workflow.ErrNodeNotFound, http.StatusNotFound, "node_not_found"},
		{workflow.ErrVariablesUnavailable, http.StatusServiceUnavailable, "variables_unavailable"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		(&WorkflowHandler{workflows: &departmentGuardedWorkflows{}, variables: &variablesStub{err: tc.err}}).NodeVariables(rec, variablesRequest())
		if rec.Code != tc.status || responsetest.CodeOf(t, rec) != tc.code {
			t.Fatalf("%v: status %d body %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}

func TestTheNodeVariablesOfAnotherDepartmentAreRefused(t *testing.T) {
	stub := &variablesStub{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &departmentGuardedWorkflows{getErr: workflow.ErrWorkflowAccessDenied}, variables: stub}).NodeVariables(rec, variablesRequest())
	if rec.Code != http.StatusForbidden || stub.wf != nil {
		t.Fatalf("status %d stub %+v", rec.Code, stub)
	}
}
