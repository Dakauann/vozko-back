package mcp

import "context"

type BuiltinBindingRepository interface {
	Upsert(ctx context.Context, b *BuiltinBinding) error

	GetByID(ctx context.Context, ws, id string) (*BuiltinBinding, error)
	ListByWorkspace(ctx context.Context, ws string) ([]*BuiltinBinding, error)

	Delete(ctx context.Context, ws, id string) error
}

type RemoteServerRepository interface {
	Create(ctx context.Context, s *RemoteMCPServer) error
	Update(ctx context.Context, s *RemoteMCPServer) error
	Get(ctx context.Context, ws, id string) (*RemoteMCPServer, error)
	ListByWorkspace(ctx context.Context, ws string) ([]*RemoteMCPServer, error)
	Delete(ctx context.Context, ws, id string) error
}

type ToolCacheRepository interface {
	Replace(ctx context.Context, sourceID, ws string, tools []CachedTool) error
	List(ctx context.Context, sourceID, ws string) ([]CachedTool, error)
	Purge(ctx context.Context, sourceID, ws string) error
}

type CollectionRepository interface {
	Create(ctx context.Context, c *MCPCollection) error
	Update(ctx context.Context, c *MCPCollection) error
	Get(ctx context.Context, ws, id string) (*MCPCollection, error)
	ListByWorkspace(ctx context.Context, ws string) ([]*MCPCollection, error)
	ListByIDs(ctx context.Context, ws string, ids []string) ([]*MCPCollection, error)
	Delete(ctx context.Context, ws, id string) error
}
