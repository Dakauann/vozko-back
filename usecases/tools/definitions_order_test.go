package tools_usecase

import (
	"context"
	"testing"

	"vozko/domain/tools"
)

type namedHandler struct{ name string }

func (h namedHandler) Definition() tools.Definition {
	return tools.Definition{Name: h.name}
}

func (h namedHandler) Execute(context.Context, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}

func TestToolDefinitionsAreListedInTheSameOrderEveryTime(t *testing.T) {
	service := NewService(namedHandler{"zeta"}, namedHandler{"alfa"}, namedHandler{"meio"})
	for round := 0; round < 20; round++ {
		for _, defs := range [][]tools.Definition{service.Definitions(), service.DefinitionsFor(tools.VisibilityMessaging)} {
			if len(defs) != 3 || defs[0].Name != "alfa" || defs[1].Name != "meio" || defs[2].Name != "zeta" {
				t.Fatalf("round %d: definitions must be sorted by name, got %v", round, defs)
			}
		}
	}
}

func (h namedHandler) ExecuteWithConfig(context.Context, map[string]interface{}, map[string]interface{}) (tools.ExecutionResult, error) {
	return tools.ExecutionResult{}, nil
}
