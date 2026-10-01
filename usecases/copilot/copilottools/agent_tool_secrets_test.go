package copilottools

import (
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

type toolDefinitions []tools.Definition

func (c toolDefinitions) Definitions() []tools.Definition { return c }
func (c toolDefinitions) DefinitionsFor(tools.ToolVisibility) []tools.Definition {
	return c
}

func httpCatalog() toolDefinitions {
	return toolDefinitions{
		{Name: "http_request", DisplayName: "Requisição HTTP", ConfigSchema: map[string]tools.ConfigParameter{
			"url":     {Type: "string"},
			"headers": {Type: "object", Sensitive: true},
		}},
		{Name: "manage_lead_memory", DisplayName: "Memórias"},
	}
}

func httpBindingArgs(headers map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id": "ag-1",
		"addTools": []interface{}{
			map[string]interface{}{"name": "http_request", "config": map[string]interface{}{"url": "https://api.exemplo.com", "headers": headers}},
			map[string]interface{}{"name": "manage_lead_memory"},
		},
	}
}

func TestEachHeaderOfAnHTTPToolBecomesAProtectedField(t *testing.T) {
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, &fakeUpdateAgent{}, AgentDeps{Catalog: httpCatalog()})
	secrets := tool.(copilot.SecretAsker).Secrets(httpBindingArgs(map[string]interface{}{"Authorization": "Bearer x", "X-Api-Key": ""}))
	if len(secrets) != 2 || secrets[0].Key != "tool:http_request:headers:Authorization" || secrets[1].Key != "tool:http_request:headers:X-Api-Key" {
		t.Fatalf("secrets = %+v", secrets)
	}
	if !strings.Contains(secrets[0].Label, "Authorization") || !strings.Contains(secrets[0].Label, "Requisição HTTP") {
		t.Fatalf("label = %q", secrets[0].Label)
	}
}

func TestHeaderValuesTheModelWroteNeverReachTheStoredProposal(t *testing.T) {
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, &fakeUpdateAgent{}, AgentDeps{Catalog: httpCatalog()})
	original := httpBindingArgs(map[string]interface{}{"Authorization": "Bearer from-the-model"})
	concealed := tool.(copilot.SecretConcealer).Conceal(original)
	raw, _ := json.Marshal(concealed)
	if strings.Contains(string(raw), "from-the-model") {
		t.Fatalf("concealed args still carry the value: %s", raw)
	}
	if !strings.Contains(string(raw), `"Authorization":""`) || !strings.Contains(string(raw), "https://api.exemplo.com") {
		t.Fatalf("names and the rest of the config must survive: %s", raw)
	}
	raw, _ = json.Marshal(original)
	if !strings.Contains(string(raw), "from-the-model") {
		t.Fatal("concealing must not mutate the caller's args")
	}
}

func TestApprovedHeaderValuesAreSavedOnTheAgent(t *testing.T) {
	upd := &fakeUpdateAgent{}
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, upd, AgentDeps{Catalog: httpCatalog()})
	args := tool.(copilot.SecretConcealer).Conceal(httpBindingArgs(map[string]interface{}{"Authorization": ""}))
	args["tool:http_request:headers:Authorization"] = "Bearer typed-on-the-card"

	if res := tool.Execute(nil, okContext(), args); res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	var saved agent.ToolBinding
	for _, b := range upd.got.InternalTools {
		if b.Name == "http_request" {
			saved = b
		}
	}
	headers, _ := saved.Config["headers"].(map[string]interface{})
	if headers["Authorization"] != "Bearer typed-on-the-card" {
		t.Fatalf("saved binding = %+v", saved)
	}
}

func TestAnInvalidHeaderNameIsRefusedBeforeTheCard(t *testing.T) {
	tool := NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, &fakeUpdateAgent{}, AgentDeps{Catalog: httpCatalog()})
	err := tool.(copilot.Validator).Validate(nil, okContext(), httpBindingArgs(map[string]interface{}{"Bad Header\r\n": ""}))
	if err == nil || !strings.Contains(err.Error(), "cabeçalho") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadingAnAgentHidesItsStoredHeaderValues(t *testing.T) {
	a := boundAgent()
	a.InternalTools = append(a.InternalTools, agent.ToolBinding{Name: "http_request", Config: map[string]interface{}{
		"url": "https://api.exemplo.com", "headers": map[string]interface{}{"Authorization": "Bearer stored-token"},
	}})
	res := NewGetAgentTool(fakeGetAgent{a: a}, httpCatalog()).Execute(nil, okContext(), map[string]interface{}{"id": "ag-1"})
	raw, _ := json.Marshal(res.Data)
	if strings.Contains(string(raw), "stored-token") {
		t.Fatalf("the model saw a stored header value: %s", raw)
	}
	if !strings.Contains(string(raw), "Authorization") || !strings.Contains(string(raw), "https://api.exemplo.com") {
		t.Fatalf("names and the rest of the config stay visible: %s", raw)
	}
	stored, _ := a.InternalTools[len(a.InternalTools)-1].Config["headers"].(map[string]interface{})
	if stored["Authorization"] != "Bearer stored-token" {
		t.Fatal("hiding values for the model must not touch the agent itself")
	}
}
