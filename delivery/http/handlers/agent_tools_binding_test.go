package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/tools"
)

type catalogOnlyTools struct {
	tools.Service
	defs []tools.Definition
}

func (c catalogOnlyTools) Definitions() []tools.Definition { return c.defs }

func TestTheToolCatalogTellsWhichConfigKeyMakesEachBindingItsOwnTool(t *testing.T) {
	h := &AgentHandler{toolRegistry: catalogOnlyTools{defs: []tools.Definition{
		{Name: "run_workflow", BindingKey: "workflow_id"},
		{Name: "http_request"},
	}}}
	rec := httptest.NewRecorder()
	h.ListTools(rec, httptest.NewRequest(http.MethodGet, "/agents/tools", nil))

	var body []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	byName := map[string]map[string]interface{}{}
	for _, tool := range body {
		byName[tool["name"].(string)] = tool
	}
	if byName["run_workflow"]["bindingKey"] != "workflow_id" {
		t.Fatalf("run_workflow = %+v", byName["run_workflow"])
	}
	if _, present := byName["http_request"]["bindingKey"]; present {
		t.Fatalf("a tool bound once per agent has no binding key: %+v", byName["http_request"])
	}
}
