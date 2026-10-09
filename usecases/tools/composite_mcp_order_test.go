package tools_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	domainmcp "vozko/domain/agent/mcp"
	ucmcp "vozko/usecases/agent/mcp"
	"vozko/usecases/agentctx"
)

type listedSource struct {
	id    string
	tools []domainmcp.Tool
}

func (s listedSource) ID() string           { return s.id }
func (s listedSource) Kind() domainmcp.Kind { return domainmcp.KindBuiltin }
func (s listedSource) DisplayName() string  { return s.id }
func (s listedSource) ListTools(context.Context, domainmcp.WorkspaceID) ([]domainmcp.Tool, error) {
	return s.tools, nil
}
func (s listedSource) CallTool(context.Context, domainmcp.WorkspaceID, string, map[string]any) (domainmcp.ToolResult, error) {
	return domainmcp.ToolResult{}, nil
}

type connectedBindings struct{ keys []string }

func (b connectedBindings) Upsert(context.Context, *domainmcp.BuiltinBinding) error { return nil }
func (b connectedBindings) GetByID(context.Context, string, string) (*domainmcp.BuiltinBinding, error) {
	return nil, domainmcp.ErrBindingNotFound
}
func (b connectedBindings) ListByWorkspace(_ context.Context, ws string) ([]*domainmcp.BuiltinBinding, error) {
	out := make([]*domainmcp.BuiltinBinding, 0, len(b.keys))
	for _, key := range b.keys {
		out = append(out, &domainmcp.BuiltinBinding{ID: key, WorkspaceID: ws, ServerKey: key, Status: domainmcp.StatusConnected})
	}
	return out, nil
}
func (b connectedBindings) Delete(context.Context, string, string) error { return nil }

type noRemotes struct{}

func (noRemotes) Create(context.Context, *domainmcp.RemoteMCPServer) error { return nil }
func (noRemotes) Update(context.Context, *domainmcp.RemoteMCPServer) error { return nil }
func (noRemotes) Get(context.Context, string, string) (*domainmcp.RemoteMCPServer, error) {
	return nil, domainmcp.ErrRemoteServerNotFound
}
func (noRemotes) ListByWorkspace(context.Context, string) ([]*domainmcp.RemoteMCPServer, error) {
	return nil, nil
}
func (noRemotes) Delete(context.Context, string, string) error { return nil }

type oneCollection struct{ members []domainmcp.CollectionMember }

func (c oneCollection) Create(context.Context, *domainmcp.MCPCollection) error { return nil }
func (c oneCollection) Update(context.Context, *domainmcp.MCPCollection) error { return nil }
func (c oneCollection) Get(context.Context, string, string) (*domainmcp.MCPCollection, error) {
	return nil, nil
}
func (c oneCollection) ListByWorkspace(context.Context, string) ([]*domainmcp.MCPCollection, error) {
	return nil, nil
}
func (c oneCollection) ListByIDs(_ context.Context, ws string, _ []string) ([]*domainmcp.MCPCollection, error) {
	return []*domainmcp.MCPCollection{{ID: "col-1", WorkspaceID: ws, Members: c.members}}, nil
}
func (c oneCollection) Delete(context.Context, string, string) error { return nil }

func builtinDescriptor(key string, tools ...string) domainmcp.BuiltinDescriptor {
	listed := make([]domainmcp.Tool, 0, len(tools))
	for _, name := range tools {
		listed = append(listed, domainmcp.Tool{Name: name, Title: name})
	}
	source := listedSource{id: "builtin:" + key, tools: listed}
	return domainmcp.BuiltinDescriptor{
		Key:      key,
		AuthSpec: domainmcp.BuiltinAuthSpec{Mode: domainmcp.AuthNone},
		Builder:  func(*domainmcp.Credential) domainmcp.ToolSource { return source },
	}
}

func TestMCPToolsAreListedByNameWhateverOrderTheSourcesAnswer(t *testing.T) {
	registry := ucmcp.NewRegistry(
		ucmcp.NewStaticCatalog(builtinDescriptor("alpha", "zeta", "beta"), builtinDescriptor("omega", "kappa")),
		connectedBindings{keys: []string{"alpha", "omega"}},
		noRemotes{},
		nil,
		nil,
	)
	composite := NewCompositeToolService(NewService(), registry, oneCollection{members: []domainmcp.CollectionMember{
		{Kind: domainmcp.CollectionMemberBuiltin, RefID: "alpha"},
		{Kind: domainmcp.CollectionMemberBuiltin, RefID: "omega"},
	}})
	ctx := agentctx.WithAgent(context.Background(), &agent.Agent{ID: "agent-1", WorkspaceID: "ws-1", MCPCollectionIDs: []string{"col-1"}})

	want := "builtin_alpha__beta,builtin_alpha__zeta,builtin_omega__kappa"
	for round := 0; round < 10; round++ {
		defs := composite.MCPDefinitionsForAgent(ctx)
		names := make([]string, 0, len(defs))
		for _, def := range defs {
			names = append(names, def.Name)
		}
		if got := strings.Join(names, ","); got != want {
			t.Fatalf("round %d: MCP tools = %s, want %s", round, got, want)
		}
	}
}
