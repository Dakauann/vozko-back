package agent_usecase

import (
	"fmt"
	"sort"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/tools"
)

var deprecatedTools = map[string]struct{}{
	"query_knowledge_base":  {},
	"conversation_analysis": {},
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

func filterExistingToolBindings(index *toolRegistryIndex, bindings []agent.ToolBinding) []agent.ToolBinding {
	if len(index.all) == 0 || len(bindings) == 0 {
		return []agent.ToolBinding{}
	}
	seen := make(map[string]struct{}, len(bindings))
	result := make([]agent.ToolBinding, 0, len(bindings))
	for _, tb := range bindings {
		normalized := strings.TrimSpace(strings.ToLower(tb.Name))
		if def, ok := index.defs[normalized]; ok {
			identity := def.BindingIdentity(tb.Config)
			if _, exists := seen[identity]; exists || identity == "" {
				continue
			}
			seen[identity] = struct{}{}
			result = append(result, agent.ToolBinding{
				Name:       def.Name,
				Visibility: tb.Visibility,
				Config:     tb.Config,
			})
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
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
		identity := def.BindingIdentity(tb.Config)
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, agent.ToolBinding{
			Name:       def.Name,
			Visibility: tb.Visibility,
			Config:     tb.Config,
		})
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}
