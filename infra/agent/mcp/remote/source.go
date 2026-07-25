package remote

import (
	"vozko/brand"
	"context"
	"errors"
	"log"
	"strings"
	"sync"

	"vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/client"
)

type Source struct {
	server        *mcp.RemoteMCPServer
	client        *client.Client
	display       string
	initOnce      sync.Once
	initErr       error
	onAuthFailure func()
}

func New(server *mcp.RemoteMCPServer, secret string) *Source {
	c := client.New(server.URL)
	if server.Credential != nil {
		switch server.Credential.Mode {
		case mcp.AuthAPIKey:
			c.APIKey = secret
		case mcp.AuthOAuth2:
			c.BearerToken = secret
		}
	}
	return &Source{server: server, client: c, display: server.Name}
}

func (s *Source) WithAuthFailureCallback(fn func()) *Source {
	s.onAuthFailure = fn
	return s
}

func (s *Source) ensureInitialized(ctx context.Context) error {
	s.initOnce.Do(func() {
		if err := s.client.Initialize(ctx, brand.Active().Key+"-mcp-client", "1.0.0"); err != nil {
			log.Printf("[MCP] Initialize failed for source %s: %v", s.ID(), err)
			s.initErr = err
			if s.onAuthFailure != nil && strings.Contains(err.Error(), "401") {
				s.onAuthFailure()
			}
		} else {
			log.Printf("[MCP] Initialize succeeded for source %s", s.ID())
		}
	})
	return s.initErr
}

func (s *Source) ID() string { return string(mcp.KindRemote) + ":" + s.server.ID }

func (s *Source) Kind() mcp.Kind { return mcp.KindRemote }

func (s *Source) DisplayName() string { return s.display }

func (s *Source) ListTools(ctx context.Context, ws mcp.WorkspaceID) ([]mcp.Tool, error) {
	if ws.Empty() {
		return nil, mcp.ErrWorkspaceRequired
	}
	if ws.String() != s.server.WorkspaceID {
		return nil, errors.New("mcp: workspace scope mismatch")
	}
	if err := s.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	return s.client.ListTools(ctx)
}

func (s *Source) CallTool(ctx context.Context, ws mcp.WorkspaceID, name string, args map[string]any) (mcp.ToolResult, error) {
	if ws.Empty() {
		return mcp.ToolResult{}, mcp.ErrWorkspaceRequired
	}
	if ws.String() != s.server.WorkspaceID {
		return mcp.ToolResult{}, errors.New("mcp: workspace scope mismatch")
	}
	if err := s.ensureInitialized(ctx); err != nil {
		return mcp.ToolResult{}, err
	}
	return s.client.CallTool(ctx, name, args)
}
