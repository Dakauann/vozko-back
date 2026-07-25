package agent_usecase

import (
	"fmt"
	"sort"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

var deprecatedTools = map[string]struct{}{
	"query_knowledge_base": {},
}

type toolRegistryIndex struct {
	defs map[string]tools.Definition
	all  []string
}

func newToolRegistryIndex(registry tools.Service) *toolRegistryIndex {
	index := &toolRegistryIndex{
		defs: make(map[string]tools.Definition),
		all:  []string{},
	}
	if registry == nil {
		return index
	}
	for _, def := range registry.Definitions() {
		normalized := strings.TrimSpace(strings.ToLower(def.Name))
		if normalized == "" {
			continue
		}
		index.defs[normalized] = def
		index.all = append(index.all, def.Name)
	}
	sort.Strings(index.all)
	return index
}

func resolveInternalToolSelection(registry tools.Service, requested []agent.ToolBinding, fallback []agent.ToolBinding) ([]agent.ToolBinding, error) {
	index := newToolRegistryIndex(registry)

	switch {
	case requested == nil && fallback == nil:
		return nil, agent.ErrAgentInternalToolsRequired
	case requested == nil:
		return filterExistingToolBindings(index, fallback), nil
	default:
		return validateRequestedToolBindings(index, requested)
	}
}

func filterExistingTools(index *toolRegistryIndex, names []string) []string {
	if len(index.all) == 0 || len(names) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(index.defs))
	for _, name := range names {
		normalized := strings.TrimSpace(strings.ToLower(name))
		if _, ok := index.defs[normalized]; ok {
			seen[index.defs[normalized].Name] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func filterExistingToolBindings(index *toolRegistryIndex, bindings []agent.ToolBinding) []agent.ToolBinding {
	if len(index.all) == 0 || len(bindings) == 0 {
		return []agent.ToolBinding{}
	}
	seen := make(map[string]struct{}, len(bindings))
	result := make([]agent.ToolBinding, 0, len(bindings))
	for _, tb := range bindings {
		normalized := strings.TrimSpace(strings.ToLower(tb.Name))
		if def, ok := index.defs[normalized]; ok {
			canonical := def.Name
			if _, exists := seen[canonical]; exists {
				continue
			}
			seen[canonical] = struct{}{}
			result = append(result, agent.ToolBinding{
				Name:       canonical,
				Visibility: tb.Visibility,
				Config:     tb.Config,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func validateRequestedTools(index *toolRegistryIndex, names []string) ([]string, error) {
	if len(names) == 0 {
		return []string{}, nil
	}
	if len(index.defs) == 0 {
		return nil, fmt.Errorf("%w: %s", agent.ErrAgentInternalToolInvalid, strings.Join(names, ", "))
	}
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		normalized := strings.TrimSpace(strings.ToLower(name))
		if normalized == "" {
			continue
		}
		if _, deprecated := deprecatedTools[normalized]; deprecated {
			continue
		}
		def, ok := index.defs[normalized]
		if !ok {
			return nil, fmt.Errorf("%w: %s", agent.ErrAgentInternalToolInvalid, name)
		}
		canonical := def.Name
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return result, nil
}

func validateRequestedToolBindings(index *toolRegistryIndex, bindings []agent.ToolBinding) ([]agent.ToolBinding, error) {
	if len(bindings) == 0 {
		return []agent.ToolBinding{}, nil
	}
	if len(index.defs) == 0 {
		names := make([]string, len(bindings))
		for i, tb := range bindings {
			names[i] = tb.Name
		}
		return nil, fmt.Errorf("%w: %s", agent.ErrAgentInternalToolInvalid, strings.Join(names, ", "))
	}
	seen := make(map[string]struct{}, len(bindings))
	result := make([]agent.ToolBinding, 0, len(bindings))
	for _, tb := range bindings {
		normalized := strings.TrimSpace(strings.ToLower(tb.Name))
		if normalized == "" {
			continue
		}
		if _, deprecated := deprecatedTools[normalized]; deprecated {
			continue
		}
		def, ok := index.defs[normalized]
		if !ok {
			return nil, fmt.Errorf("%w: %s", agent.ErrAgentInternalToolInvalid, tb.Name)
		}
		if err := def.ValidateConfig(tb.Config); err != nil {
			return nil, fmt.Errorf("%w: %s config invalid: %v", agent.ErrAgentInternalToolInvalid, def.Name, err)
		}
		canonical := def.Name
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, agent.ToolBinding{
			Name:       canonical,
			Visibility: tb.Visibility,
			Config:     tb.Config,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}
