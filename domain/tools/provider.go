package tools

import "context"

type ProviderService interface {
	CreateClientTool(ctx context.Context, input ProviderToolInput) (ProviderTool, error)
}

type ProviderToolInput struct {
	AgentID    string
	AgentName  string
	Definition Definition
}

type ProviderTool struct {
	ID string
}
