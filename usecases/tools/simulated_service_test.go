package tools_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/tools"
)

// explodingService stands in for the production registry: any execution
// reaching it means the sandbox leaked a side effect. recordingService below
// is the opposite check, for the tools that are SUPPOSED to reach it.
type explodingService struct {
	defs []tools.Definition
}

func (e explodingService) Definitions() []tools.Definition                        { return e.defs }
func (e explodingService) DefinitionsFor(tools.ToolVisibility) []tools.Definition { return e.defs }
func (e explodingService) Handler(string) (tools.Handler, bool)                   { return nil, false }
func (e explodingService) Execute(context.Context, string, map[string]interface{}) (tools.ExecutionResult, error) {
	panic("side effect escaped the simulation sandbox")
}
func (e explodingService) ExecuteWithConfig(context.Context, string, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	panic("side effect escaped the simulation sandbox")
}

type recordingService struct {
	explodingService
	executed string
}

func (r *recordingService) Execute(_ context.Context, name string, _ map[string]interface{}) (tools.ExecutionResult, error) {
	r.executed = name
	return tools.ExecutionResult{Result: "resultado real"}, nil
}
func (r *recordingService) ExecuteWithConfig(_ context.Context, name string, _, _ map[string]interface{}) (tools.ExecutionResult, error) {
	r.executed = name
	return tools.ExecutionResult{Result: "resultado real"}, nil
}

func TestSimulatedServiceStubsEverythingThatCouldChangeSomething(t *testing.T) {
	sandbox := NewSimulatedToolService(explodingService{})

	res, err := sandbox.Execute(context.Background(), "manage_entry_stage", map[string]interface{}{"target_tag_name": "ganho"})
	if err != nil || res.IsError {
		t.Fatalf("sandboxed execute = (%+v, %v)", res, err)
	}
	res, err = sandbox.ExecuteWithConfig(context.Background(), "send_whatsapp_media", map[string]interface{}{"__entry_id": "e1"}, nil)
	if err != nil || res.IsError {
		t.Fatalf("sandboxed executeWithConfig = (%+v, %v)", res, err)
	}

	// The canned result must declare itself simulated AND tell the model to
	// proceed: a result that reads like a failure would derail the turn.
	text, _ := res.Result.(string)
	if !strings.Contains(text, "SIMULAÇÃO") || !strings.Contains(text, "send_whatsapp_media") {
		t.Fatalf("canned result = %q", text)
	}

	// An MCP tool is unknown to the native registry, so it cannot be
	// classified and must be stubbed rather than called.
	if _, err := sandbox.ExecuteWithConfig(context.Background(), "remote_x__create_issue", nil, nil); err != nil {
		t.Fatalf("mcp tool = %v", err)
	}
}

func TestSimulatedServiceRunsRetrievalForReal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tool   string
		config map[string]interface{}
	}{
		{"knowledge base", SearchKnowledgeBaseToolName, nil},
		{"calendar availability", CheckCalendarAvailabilityToolName, map[string]interface{}{"__workspace_id": "ws-1"}},
		{"cep", ValidateCEPToolName, nil},
		{"http GET", httpRequestToolName, map[string]interface{}{"method": "get", "url": "https://x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			real := &recordingService{}
			sandbox := NewSimulatedToolService(real)

			res, err := sandbox.ExecuteWithConfig(context.Background(), tc.tool, tc.config, nil)
			if err != nil {
				t.Fatalf("execute = %v", err)
			}
			if real.executed != tc.tool {
				t.Fatalf("tool did not reach the real registry: executed=%q", real.executed)
			}
			if got, _ := res.Result.(string); got != "resultado real" {
				t.Fatalf("result = %q, want the real registry's", got)
			}
		})
	}
}

// http_request is the one tool whose safety depends on its configuration, so
// only the unambiguously safe verbs may reach the network for real.
func TestSimulationRunsForRealHTTPMethods(t *testing.T) {
	for method, want := range map[string]bool{
		"GET": true, "get": true, " HEAD ": true,
		"POST": false, "PUT": false, "PATCH": false, "DELETE": false, "": false, "nonsense": false,
	} {
		if got := SimulationRunsForReal(httpRequestToolName, map[string]interface{}{"method": method}); got != want {
			t.Errorf("method %q: runs for real = %v, want %v", method, got, want)
		}
	}
	// A missing config cannot be proven safe.
	if SimulationRunsForReal(httpRequestToolName, nil) {
		t.Error("http_request with no config must not run for real")
	}
}

func TestSimulationRunsForRealIsClosedByDefault(t *testing.T) {
	for _, name := range []string{
		ManageLeadMemoryToolName, ManageEntryStageToolName, FinishConversationToolName,
		ScheduleMeetingToolName, RescheduleMeetingToolName, SendEmailToolName,
		ConversationAnalysisToolName, "generate_payment", "send_whatsapp_media",
		"remote_x__anything", "tool_the_model_invented", "",
	} {
		if SimulationRunsForReal(name, map[string]interface{}{"method": "GET"}) {
			t.Errorf("%q must be stubbed in the simulator", name)
		}
	}
}

func TestSimulatedServicePresentsTheRealToolSet(t *testing.T) {
	sandbox := NewSimulatedToolService(explodingService{defs: []tools.Definition{{Name: "manage_lead_memory"}}})

	defs := sandbox.Definitions()
	if len(defs) != 1 || defs[0].Name != "manage_lead_memory" {
		t.Fatalf("definitions not delegated: %+v", defs)
	}
	if got := sandbox.DefinitionsFor(tools.VisibilityMessaging); len(got) != 1 {
		t.Fatalf("DefinitionsFor not delegated: %+v", got)
	}
}
