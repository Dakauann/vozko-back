package copilot_usecase

import (
	"slices"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
)

type Registry struct {
	byName  map[string]copilot.Tool
	ordered []copilot.Tool
}

func NewRegistry(ts ...copilot.Tool) *Registry {
	r := &Registry{byName: make(map[string]copilot.Tool, len(ts))}
	for _, t := range ts {
		if t == nil {
			continue
		}
		r.byName[t.Definition().Name] = t
	}
	for _, t := range r.byName {
		r.ordered = append(r.ordered, t)
	}
	slices.SortFunc(r.ordered, func(a, b copilot.Tool) int { return strings.Compare(a.Definition().Name, b.Definition().Name) })
	return r
}

func (r *Registry) Get(name string) (copilot.Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

func (r *Registry) Tools() []copilot.Tool {
	return slices.Clone(r.ordered)
}

func (r *Registry) Definitions() []tools.Definition {
	defs := make([]tools.Definition, 0, len(r.ordered))
	for _, t := range r.ordered {
		defs = append(defs, t.Definition())
	}
	return defs
}
