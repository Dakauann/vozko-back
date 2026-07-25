package agent_usecase

import (
	"strings"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

// syncAgentTools reconciles the agent's tool-binding map with its selected
// internal tools, dropping bindings for tools that are no longer selected.
func syncAgentTools(registry tools.Service, a *agent.Agent) error {
	_ = registry
	if a == nil {
		return agent.ErrAgentNameRequired
	}
	if a.ToolBindings == nil {
		a.ToolBindings = make(map[string]string)
	}

	selected := make(map[string]struct{}, len(a.InternalTools))
	for _, tb := range a.InternalTools {
		trimmed := strings.TrimSpace(tb.Name)
		if trimmed == "" {
			continue
		}
		selected[trimmed] = struct{}{}
	}

	for key := range a.ToolBindings {
		if _, ok := selected[key]; !ok || len(selected) == 0 {
			delete(a.ToolBindings, key)
		}
	}

	for name := range selected {
		if _, exists := a.ToolBindings[name]; !exists {
			a.ToolBindings[name] = ""
		}
	}

	return nil
}
