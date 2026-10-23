package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

type versionReadingWorkflows struct {
	workflow.ScopedWorkflowsUseCase
	asked int
}

func (f *versionReadingWorkflows) Version(_ workflow.Scope, id string, version int) (*workflow.WorkflowVersion, error) {
	f.asked = version
	if version == 9 {
		return nil, fmt.Errorf("%w: workflow %s version %d", workflow.ErrVersionNotFound, id, version)
	}
	g := workflow.Graph{Nodes: []workflow.Node{{ID: "t", Type: workflow.NodeTypeTriggerMessageReceived}}}
	return &workflow.WorkflowVersion{WorkflowID: id, Version: version, Graph: &g}, nil
}

func TestReadingOneVersionAnswersItsGraph(t *testing.T) {
	workflows := &versionReadingWorkflows{}
	rec := httptest.NewRecorder()
	r := mux.SetURLVars(workflowRequestAs(http.MethodGet, "/workflows/w1/versions/3", nil, &auth.Claims{UserID: "u1"}), map[string]string{"id": "w1", "version": "3"})
	(&WorkflowHandler{workflows: workflows, catalogFn: func() []workflow.NodeDefinition { return nil }}).GetVersion(rec, r)

	if rec.Code != http.StatusOK || workflows.asked != 3 {
		t.Fatalf("status = %d asked = %d body = %s", rec.Code, workflows.asked, rec.Body.String())
	}
	var body struct {
		Version int             `json:"version"`
		Graph   *workflow.Graph `json:"graph"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Version != 3 || body.Graph == nil || len(body.Graph.Nodes) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}

	for version, want := range map[string]int{"9": http.StatusNotFound, "x": http.StatusBadRequest} {
		rec := httptest.NewRecorder()
		r := mux.SetURLVars(workflowRequestAs(http.MethodGet, "/workflows/w1/versions/"+version, nil, &auth.Claims{UserID: "u1"}), map[string]string{"id": "w1", "version": version})
		(&WorkflowHandler{workflows: workflows, catalogFn: func() []workflow.NodeDefinition { return nil }}).GetVersion(rec, r)
		if rec.Code != want {
			t.Errorf("version %s: status = %d, want %d", version, rec.Code, want)
		}
	}
}

func (f *versionReadingWorkflows) Get(_ workflow.Scope, id string) (*workflow.Workflow, error) {
	return &workflow.Workflow{ID: id, Name: "Fluxo", Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "h", Type: workflow.NodeTypeActionHTTPRequest, Pinned: &workflow.NodePin{Output: map[string]interface{}{"cpf": "•••• 25"}, Source: workflow.PinSourceOfRun("r-1")}},
	}}}, nil
}

func TestAnExportedWorkflowCarriesNoPinnedTestData(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WorkflowHandler{workflows: &versionReadingWorkflows{}}).Export(rec, workflowRequestAs(http.MethodGet, "/workflows/w1/export", nil, &auth.Claims{UserID: "u1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Graph workflow.Graph `json:"graph"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Graph.Nodes) != 1 || body.Graph.HasPins() {
		t.Fatalf("graph = %+v, an export moves the workflow, not its test data", body.Graph)
	}
}

type pinnerSpy struct {
	in     workflow_usecase.PinFromRunInput
	viewer shared.Person
	err    error
}

func (p *pinnerSpy) PinFromRun(_ context.Context, _ workflow.Scope, viewer shared.Person, in workflow_usecase.PinFromRunInput) (*workflow.Workflow, error) {
	p.in, p.viewer = in, viewer
	if p.err != nil {
		return nil, p.err
	}
	return &workflow.Workflow{ID: in.WorkflowID}, nil
}

func pinRequest(body string) *http.Request {
	r := workflowRequestAs(http.MethodPost, "/workflows/w1/nodes/n55/pin-from-run", []byte(body), &auth.Claims{UserID: "u1"})
	return mux.SetURLVars(r, map[string]string{"id": "w1", "nodeId": "n55"})
}

func TestPinningFromARunPassesTheNodeRunAndPerson(t *testing.T) {
	spy := &pinnerSpy{}
	rec := httptest.NewRecorder()
	(&WorkflowHandler{nodePins: spy}).PinFromRun(rec, pinRequest(`{"runId":"r-9","stepId":"s-2","revision":2}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if spy.in != (workflow_usecase.PinFromRunInput{WorkflowID: "w1", NodeID: "n55", RunID: "r-9", StepID: "s-2", Revision: 2}) || spy.viewer.UserID != "u1" {
		t.Fatalf("in = %+v viewer = %+v", spy.in, spy.viewer)
	}
}

func TestPinningFromARunMapsRefusals(t *testing.T) {
	cases := []struct {
		body string
		err  error
		want int
	}{
		{`{}`, nil, http.StatusBadRequest},
		{`not json`, nil, http.StatusBadRequest},
		{`{"runId":"r-9","revision":1}`, workflow.ErrRunNotFound, http.StatusNotFound},
		{`{"runId":"r-9","revision":1}`, workflow.ErrPinStepUnusable, http.StatusUnprocessableEntity},
		{`{"runId":"r-9","revision":1}`, workflow.ErrPinNodeNotFound, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		(&WorkflowHandler{nodePins: &pinnerSpy{err: tc.err}}).PinFromRun(rec, pinRequest(tc.body))
		if rec.Code != tc.want {
			t.Errorf("%s / %v: status = %d, want %d", tc.body, tc.err, rec.Code, tc.want)
		}
	}
}
