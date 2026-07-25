package tools_usecase

import (
	"context"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/tools"
	"vozko/usecases/agentctx"
)

type ResolvedToolSet struct {
	Definitions []tools.Definition
	Configs     map[string]map[string]interface{}
}

type ToolResolverOptions struct {
	Agent        *agent.Agent
	CampaignID   string
	CampaignType string
}

func ResolveTools(
	registry tools.Service,
	bindings []agent.ToolBinding,
	visibility agent.ToolVisibility,
	opts ToolResolverOptions,
) ResolvedToolSet {
	if registry == nil || len(bindings) == 0 {
		return ResolvedToolSet{}
	}

	allDefs := registry.Definitions()
	defIndex := make(map[string]tools.Definition, len(allDefs))
	for _, def := range allDefs {
		key := strings.ToLower(strings.TrimSpace(def.Name))
		if key != "" {
			defIndex[key] = def
		}
	}

	toolCtx := tools.ToolContext{}
	if opts.Agent != nil {
		toolCtx.Agent = opts.Agent
		toolCtx.WorkspaceID = opts.Agent.WorkspaceID
	}
	if opts.CampaignID != "" {
		toolCtx.CampaignID = opts.CampaignID
		toolCtx.CampaignType = opts.CampaignType
	}

	result := make([]tools.Definition, 0, len(bindings))
	configsMap := make(map[string]map[string]interface{})
	seen := make(map[string]struct{}, len(bindings))

	for _, binding := range bindings {
		key := strings.ToLower(strings.TrimSpace(binding.Name))
		if key == "" {
			continue
		}

		def, ok := defIndex[key]
		if !ok {
			continue
		}

		canonical := strings.TrimSpace(def.Name)
		if _, exists := seen[canonical]; exists {
			continue
		}

		isVisible := def.IsVisibleIn(tools.ToolVisibility(visibility))
		if isVisible && len(binding.Visibility) > 0 {
			bindingAllows := false
			for _, v := range binding.Visibility {
				if v == visibility {
					bindingAllows = true
					break
				}
			}
			isVisible = bindingAllows
		}

		if !isVisible {
			continue
		}

		seen[canonical] = struct{}{}

		expandedDef := def
		if len(binding.Config) > 0 {
			expandedDef = def.WithConfigExpansion(binding.Config)
			configsMap[strings.ToLower(canonical)] = binding.Config
		}

		h, hOk := registry.Handler(strings.ToLower(key))
		if hOk {
			if ch, ok := h.(tools.ContextualHandler); ok {
				expandedDef = ch.DefinitionWithContext(toolCtx)

				if len(binding.Config) > 0 {
					expandedDef = expandedDef.WithConfigExpansion(binding.Config)
				}
			}
		}

		result = append(result, expandedDef)
	}

	return ResolvedToolSet{
		Definitions: result,
		Configs:     configsMap,
	}
}

func ResolveToolsForVisibility(
	registry tools.Service,
	bindings []agent.ToolBinding,
	visibility agent.ToolVisibility,
) ResolvedToolSet {
	return ResolveToolsWithOptions(registry, bindings, visibility, ToolResolverOptions{})
}

func ResolveToolsWithOptions(
	registry tools.Service,
	bindings []agent.ToolBinding,
	visibility agent.ToolVisibility,
	opts ToolResolverOptions,
) ResolvedToolSet {
	result := ResolveTools(registry, bindings, visibility, opts)
	if composite, ok := registry.(interface {
		MCPDefinitionsForAgent(ctx context.Context) []tools.Definition
	}); ok && opts.Agent != nil && opts.Agent.HasMCPCollections() {
		ctx := agentctx.WithAgent(context.Background(), opts.Agent)
		mcpDefs := composite.MCPDefinitionsForAgent(ctx)
		result.Definitions = append(result.Definitions, mcpDefs...)
	}
	return result
}

func PatchDefinitionsForWorkspace(defs []tools.Definition, workspaceID string, registry tools.Service) []tools.Definition {
	if workspaceID == "" || registry == nil || len(defs) == 0 {
		return defs
	}
	ctx := tools.ToolContext{WorkspaceID: workspaceID}
	result := make([]tools.Definition, len(defs))
	copy(result, defs)
	for i, def := range result {
		h, ok := registry.Handler(strings.ToLower(strings.TrimSpace(def.Name)))
		if !ok {
			continue
		}
		if ch, ok := h.(tools.ContextualHandler); ok {
			result[i] = ch.DefinitionWithContext(ctx)
		}
	}
	return result
}
