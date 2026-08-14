package tools_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/tools"
)

// explodingService stands in for the production registry: any execution
// reaching it means the sandbox leaked a side effect.
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

func TestSimulatedServiceNeverExecutesForReal(t *testing.T) {
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
