// Package copilot_usecase implements the dashboard AI copilot on top of the shared
// agentloop harness. It is a SEPARATE tool registry from the voice/messaging
// runtime tools (domain/tools): a customer-facing agent must never be able to
// invoke a management tool like create_agent, and separate registries make that
// impossible by construction.
package copilot_usecase

import (
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

// Registry is the copilot's tool catalog, keyed by tool name.
type Registry struct {
	byName map[string]copilot.Tool
}

// NewRegistry builds a registry from the given tools. Later registrations win on a
// name clash (kept simple; names are expected to be unique).
func NewRegistry(ts ...copilot.Tool) *Registry {
	r := &Registry{byName: make(map[string]copilot.Tool, len(ts))}
	for _, t := range ts {
		if t == nil {
			continue
		}
		r.byName[t.Definition().Name] = t
	}
	return r
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (copilot.Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// Definitions returns the model-facing definitions of every registered tool.
func (r *Registry) Definitions() []tools.Definition {
	defs := make([]tools.Definition, 0, len(r.byName))
	for _, t := range r.byName {
		defs = append(defs, t.Definition())
	}
	return defs
}
