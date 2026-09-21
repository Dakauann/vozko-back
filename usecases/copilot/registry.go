package copilot_usecase

import (
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

type Registry struct {
	byName map[string]copilot.Tool
}

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

func (r *Registry) Get(name string) (copilot.Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

func (r *Registry) Definitions() []tools.Definition {
	defs := make([]tools.Definition, 0, len(r.byName))
	for _, t := range r.byName {
		defs = append(defs, t.Definition())
	}
	return defs
}
