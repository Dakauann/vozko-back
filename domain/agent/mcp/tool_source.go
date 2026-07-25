package mcp

import "context"

type ToolSource interface {
	ID() string
	Kind() Kind
	DisplayName() string

	ListTools(ctx context.Context, ws WorkspaceID) ([]Tool, error)

	CallTool(ctx context.Context, ws WorkspaceID, name string, args map[string]any) (ToolResult, error)
}
