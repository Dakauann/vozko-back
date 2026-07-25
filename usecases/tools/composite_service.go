package tools_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/domain/tools"
	ucmcp "vozko/usecases/agent/mcp"
	"vozko/usecases/agentctx"
)

func sanitizeToolName(raw string) string {
	r := strings.ReplaceAll(raw, ":", "_")
	parts := strings.SplitN(r, ".", 2)
	if len(parts) != 2 {
		return r
	}
	maxPrefix := 7 + 8
	if len(parts[0]) > maxPrefix {
		parts[0] = parts[0][:maxPrefix]
	}
	result := parts[0] + "__" + parts[1]
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

func unsanitizeToolName(sanitized string) string {
	idx := strings.LastIndex(sanitized, "__")
	if idx < 0 {
		return sanitized
	}
	prefix := sanitized[:idx]
	tool := sanitized[idx+2:]
	colonIdx := strings.IndexByte(prefix, '_')
	if colonIdx > 0 {
		prefix = prefix[:colonIdx] + ":" + prefix[colonIdx+1:]
	}
	return prefix + "." + tool
}

type CompositeToolService struct {
	native  tools.Service
	mcpReg  *ucmcp.Registry
	mcpRepo domainmcp.CollectionRepository
}

func NewCompositeToolService(native tools.Service, mcpReg *ucmcp.Registry, mcpRepo domainmcp.CollectionRepository) *CompositeToolService {
	return &CompositeToolService{native: native, mcpReg: mcpReg, mcpRepo: mcpRepo}
}

func (c *CompositeToolService) Definitions() []tools.Definition {
	return c.native.Definitions()
}

func (c *CompositeToolService) DefinitionsFor(visibility tools.ToolVisibility) []tools.Definition {
	return c.native.DefinitionsFor(visibility)
}

func (c *CompositeToolService) Execute(ctx context.Context, name string, params map[string]interface{}) (tools.ExecutionResult, error) {
	raw := unsanitizeToolName(name)
	qn, err := domainmcp.ParseQualifiedName(raw)
	if err == nil {
		return c.executeMCP(ctx, qn, params)
	}
	return c.native.Execute(ctx, name, params)
}

func (c *CompositeToolService) ExecuteWithConfig(ctx context.Context, name string, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	raw := unsanitizeToolName(name)
	qn, err := domainmcp.ParseQualifiedName(raw)
	if err == nil {
		return c.executeMCP(ctx, qn, params)
	}
	return c.native.ExecuteWithConfig(ctx, name, config, params)
}

func (c *CompositeToolService) Handler(name string) (tools.Handler, bool) {
	return c.native.Handler(name)
}

func (c *CompositeToolService) Register(handler tools.Handler) {
	if svc, ok := c.native.(*Service); ok {
		svc.Register(handler)
	}
}

func (c *CompositeToolService) MCPDefinitionsForAgent(ctx context.Context) []tools.Definition {
	return c.mcpDefinitionsForAgent(ctx)
}

func (c *CompositeToolService) mcpDefinitionsForAgent(ctx context.Context) []tools.Definition {
	agent, ok := agentctx.AgentFromContext(ctx)
	if !ok || agent == nil || !agent.HasMCPCollections() {
		return nil
	}
	sources, err := c.mcpReg.SourcesForCollections(ctx, domainmcp.WorkspaceID(agent.WorkspaceID), c.mcpRepo, agent.MCPCollectionIDs)
	if err != nil {
		log.Printf("[MCP-Tool] SourcesForCollections failed: %v", err)
		return nil
	}
	if len(sources) == 0 {
		return nil
	}

	type item struct {
		sanitizedName string
		displayName   string
		description   string
		params        map[string]tools.Parameter
		required      []string
	}

	var mu sync.Mutex
	var items []item

	g, gctx := errgroup.WithContext(ctx)
	for _, s := range sources {
		s := s
		g.Go(func() error {
			ts, err := s.ListTools(gctx, domainmcp.WorkspaceID(agent.WorkspaceID))
			if err != nil {
				log.Printf("[MCP-Tool] ListTools failed for source %s: %v", s.ID(), err)
				return nil
			}
			mu.Lock()
			for _, t := range ts {
				qualifiedName := s.ID() + "." + t.Name
				params, required := parseSchemaToParams(t.InputSchema)
				sanitizedName := sanitizeToolName(qualifiedName)
				log.Printf("[MCP-Tool] definition: name=%s params=%d required=%d schema_len=%d", sanitizedName, len(params), len(required), len(t.InputSchema))
				items = append(items, item{
					sanitizedName: sanitizedName,
					displayName:   "[" + s.DisplayName() + "] " + t.Title,
					description:   t.Description,
					params:        params,
					required:      required,
				})
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	defs := make([]tools.Definition, 0, len(items))
	for _, it := range items {
		defs = append(defs, tools.Definition{
			Name:        it.sanitizedName,
			DisplayName: it.displayName,
			Description: it.description,
			Parameters:  it.params,
			Required:    it.required,
		})
	}
	return defs
}

const mcpMaxResultChars = 10000

func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n\n[... result truncated to %d characters ...]", maxLen)
}

func (c *CompositeToolService) executeMCP(ctx context.Context, qn domainmcp.QualifiedName, params map[string]interface{}) (tools.ExecutionResult, error) {
	agent, _ := agentctx.AgentFromContext(ctx)
	wsID := domainmcp.WorkspaceID("")
	if agent != nil {
		wsID = domainmcp.WorkspaceID(agent.WorkspaceID)
	}
	source, localName, err := c.resolveTruncated(ctx, wsID, qn)
	if err != nil {
		log.Printf("[MCP-Tool] resolve failed: qn=%s ws=%s err=%v", qn.String(), wsID, err)
		return tools.ExecutionResult{}, err
	}
	log.Printf("[MCP-Tool] calling source=%s tool=%s", source.ID(), localName)
	result, err := source.CallTool(ctx, wsID, localName, params)
	if err != nil {
		log.Printf("[MCP-Tool] call failed: source=%s tool=%s err=%v", source.ID(), localName, err)
		errMsg := truncateText(err.Error(), mcpMaxResultChars)
		return tools.ExecutionResult{IsError: true, Result: errMsg, ContextUpdateText: errMsg}, nil
	}
	if result.IsError {
		log.Printf("[MCP-Tool] tool error: source=%s tool=%s content=%+v", source.ID(), localName, result.Content)
		msg := ""
		if len(result.Content) > 0 {
			msg = truncateText(result.Content[0].Text, mcpMaxResultChars)
		}
		return tools.ExecutionResult{IsError: true, Result: msg, ContextUpdateText: msg}, nil
	}
	text := ""
	if len(result.Content) > 0 {
		text = truncateText(result.Content[0].Text, mcpMaxResultChars)
	}
	return tools.ExecutionResult{Result: map[string]interface{}{"text": text}, ContextUpdateText: text}, nil
}

func (c *CompositeToolService) resolveTruncated(ctx context.Context, wsID domainmcp.WorkspaceID, qn domainmcp.QualifiedName) (domainmcp.ToolSource, string, error) {
	source, localName, err := c.mcpReg.Resolve(ctx, wsID, qn.String())
	if err == nil {
		return source, localName, nil
	}
	if qn.Kind == domainmcp.KindRemote || qn.Kind == domainmcp.KindBuiltin {
		sources, srcErr := c.mcpReg.Sources(ctx, wsID)
		if srcErr == nil {
			prefix := string(qn.Kind) + ":" + qn.SourceID
			for _, s := range sources {
				if len(s.ID()) >= len(prefix) && s.ID()[:len(prefix)] == prefix {
					return s, qn.Tool, nil
				}
			}
		}
	}
	return nil, "", err
}

func parseSchemaToParams(schema []byte) (map[string]tools.Parameter, []string) {
	if len(schema) == 0 {
		return nil, nil
	}
	var raw struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &raw); err != nil {
		return nil, nil
	}
	if len(raw.Properties) == 0 {
		return nil, nil
	}
	params := make(map[string]tools.Parameter, len(raw.Properties))
	for name, propRaw := range raw.Properties {
		var prop struct {
			Type        string                     `json:"type"`
			Description string                     `json:"description"`
			Enum        []string                   `json:"enum"`
			Items       json.RawMessage            `json:"items"`
			Properties  map[string]json.RawMessage `json:"properties"`
		}
		p := tools.Parameter{}
		if err := json.Unmarshal(propRaw, &prop); err != nil {
			p.Type = "string"
			params[name] = p
			continue
		}
		switch prop.Type {
		case "", "null":
			p.Type = "string"
		case "array":
			p.Type = "array"
		case "object":
			p.Type = "object"
			if len(prop.Properties) > 0 {
				itemProps := make(map[string]tools.Parameter, len(prop.Properties))
				for ik, iRaw := range prop.Properties {
					var ip struct {
						Type        string   `json:"type"`
						Description string   `json:"description"`
						Enum        []string `json:"enum"`
					}
					var ipParam tools.Parameter
					if err := json.Unmarshal(iRaw, &ip); err != nil {
						ipParam.Type = "string"
					} else {
						switch ip.Type {
						case "", "null":
							ipParam.Type = "string"
						default:
							ipParam.Type = ip.Type
						}
						ipParam.Description = ip.Description
						ipParam.Enum = ip.Enum
					}
					itemProps[ik] = ipParam
				}
				p.Items = &tools.ParameterItems{
					Type:       "object",
					Properties: itemProps,
				}
			}
		default:
			p.Type = prop.Type
		}
		p.Description = prop.Description
		p.Enum = prop.Enum
		if len(prop.Items) > 0 && p.Type == "array" {
			var item struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(prop.Items, &item); err == nil {
				it := ""
				switch item.Type {
				case "", "null":
					it = "string"
				default:
					it = item.Type
				}
				p.Items = &tools.ParameterItems{
					Type:        it,
					Description: item.Description,
				}
			}
		}
		params[name] = p
	}
	return params, raw.Required
}
