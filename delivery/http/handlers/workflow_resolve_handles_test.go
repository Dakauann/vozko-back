package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/workflow"
)

func postResolveHandles(t *testing.T, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r := workflowRequestAs(http.MethodPost, "/workflows/resolve-handles", body, &auth.Claims{UserID: "u1"})
	(&WorkflowHandler{catalogFn: func() []workflow.NodeDefinition { return nil }}).ResolveHandles(rec, r)
	return rec
}

func nodesBody(t *testing.T, count int, config map[string]interface{}) []byte {
	t.Helper()
	nodes := make([]workflow.Node, count)
	for i := range nodes {
		nodes[i] = workflow.Node{ID: fmt.Sprintf("n%d", i), Type: workflow.NodeTypeActionSendInteractive, Config: config}
	}
	body, err := json.Marshal(map[string]interface{}{"nodes": nodes})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func refusalCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Code
}

func TestResolveHandlesAnswersTheOptionsOfTheCanonicalList(t *testing.T) {
	rec := postResolveHandles(t, nodesBody(t, 1, map[string]interface{}{
		"interactive_type": "list",
		"sections":         `[{"title":"Aulas Aquáticas","rows":[{"id":"1","title":"Horários","description":""},{"id":"2","title":"Materiais","description":""}]}]`,
	}))

	var body struct {
		Handles map[string][]workflow.HandleDefinition `json:"handles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	ids := []string{}
	for _, h := range body.Handles["n0"] {
		ids = append(ids, h.ID)
	}
	if strings.Join(ids, ",") != "1,2,no_match,no_reply,send_failed" {
		t.Fatalf("handles = %v", ids)
	}
}

func TestResolveHandlesRefusesMoreNodesThanAWorkflowCanHold(t *testing.T) {
	rec := postResolveHandles(t, nodesBody(t, workflow.MaxNodesPerWorkflow+1, map[string]interface{}{}))

	if rec.Code != http.StatusUnprocessableEntity || refusalCode(t, rec) != "graph_too_many_nodes" {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestResolveHandlesRefusesABodyLargerThanTheLargestImport(t *testing.T) {
	huge := strings.Repeat("x", maxImportSize)
	rec := postResolveHandles(t, nodesBody(t, 1, map[string]interface{}{"body": huge}))

	if rec.Code != http.StatusBadRequest || refusalCode(t, rec) != "body_too_large" {
		t.Fatalf("status = %d code = %s", rec.Code, refusalCode(t, rec))
	}
}

type detailedWorkflow struct {
	workflow.ScopedWorkflowsUseCase
	wf *workflow.Workflow
}

func (f *detailedWorkflow) Detail(workflow.Scope, string) (*workflow.Workflow, error) {
	return f.wf, nil
}

func TestOpeningAWorkflowAnswersTheHandlesOfItsDynamicNodes(t *testing.T) {
	catalog := []workflow.NodeDefinition{
		{Type: workflow.NodeTypeActionSendInteractive, DynamicHandles: true},
		{Type: workflow.NodeTypeActionSendText},
	}
	wf := &workflow.Workflow{ID: "w1", Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "n27", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
			"interactive_type": "list",
			"sections":         `[{"title":"Aulas","rows":[{"id":"1","title":"Um","description":""},{"id":"2","title":"Dois","description":""}]}]`,
		}},
		{ID: "txt", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{}},
	}}}
	rec := httptest.NewRecorder()
	r := mux.SetURLVars(workflowRequestAs(http.MethodGet, "/workflows/w1", nil, &auth.Claims{UserID: "u1"}), map[string]string{"id": "w1"})
	(&WorkflowHandler{workflows: &detailedWorkflow{wf: wf}, catalogFn: func() []workflow.NodeDefinition { return catalog }}).Get(rec, r)

	var body struct {
		Handles map[string][]workflow.HandleDefinition `json:"handles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, static := body.Handles["txt"]; static || len(body.Handles) != 1 {
		t.Fatalf("handles = %v, want only the dynamic node", body.Handles)
	}
	if got := body.Handles["n27"]; len(got) != 5 || got[0].ID != "1" || got[1].ID != "2" {
		t.Fatalf("n27 handles = %v", got)
	}
}

type versionWithList struct {
	workflow.ScopedWorkflowsUseCase
}

func (versionWithList) Version(_ workflow.Scope, id string, version int) (*workflow.WorkflowVersion, error) {
	g := workflow.Graph{Nodes: []workflow.Node{{ID: "n27", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
		"interactive_type": "list",
		"sections":         `[{"title":"Aulas","rows":[{"id":"1","title":"Um","description":""}]}]`,
	}}}}
	return &workflow.WorkflowVersion{WorkflowID: id, Version: version, Graph: &g}, nil
}

func TestReadingAVersionAnswersTheHandlesItsGraphHad(t *testing.T) {
	rec := httptest.NewRecorder()
	r := mux.SetURLVars(workflowRequestAs(http.MethodGet, "/workflows/w1/versions/2", nil, &auth.Claims{UserID: "u1"}), map[string]string{"id": "w1", "version": "2"})
	catalog := []workflow.NodeDefinition{{Type: workflow.NodeTypeActionSendInteractive, DynamicHandles: true}}
	(&WorkflowHandler{workflows: versionWithList{}, catalogFn: func() []workflow.NodeDefinition { return catalog }}).GetVersion(rec, r)

	var body struct {
		Handles map[string][]workflow.HandleDefinition `json:"handles"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if got := body.Handles["n27"]; len(got) == 0 || got[0].ID != "1" {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
