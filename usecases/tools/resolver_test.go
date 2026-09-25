package tools_usecase

import (
	"context"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

type configEchoTool struct {
	seen map[string]interface{}
}

func (t *configEchoTool) Definition() tools.Definition {
	return tools.Definition{Name: "config_echo", Visibility: []tools.ToolVisibility{tools.VisibilityMessaging}}
}

func (t *configEchoTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	t.seen = ctx.Config
	return t.Definition()
}

func (t *configEchoTool) Execute(context.Context, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}

func (t *configEchoTool) ExecuteWithConfig(context.Context, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}

func TestResolveToolsHandsEachToolItsOwnConfig(t *testing.T) {
	echo := &configEchoTool{}
	ResolveTools(NewService(echo), []agent.ToolBinding{
		{Name: "config_echo", Config: map[string]interface{}{"pipeline_id": "pipe1"}},
	}, agent.ToolVisibility(tools.VisibilityMessaging), ToolResolverOptions{})

	if echo.seen["pipeline_id"] != "pipe1" {
		t.Fatalf("DefinitionWithContext saw config %v, want the binding's pipeline", echo.seen)
	}
}
