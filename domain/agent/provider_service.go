package agent

import "context"

type ProviderService interface {
	Create(ctx context.Context, input ProviderAgentInput) (ProviderAgent, error)
	Update(ctx context.Context, externalID string, input ProviderAgentInput) (ProviderAgent, error)
}

type ProviderAgentInput struct {
	Name           string
	Description    string
	InitialMessage string
	Metadata       map[string]string
	Tags           []string
	ToolIDs        []string
}

type ProviderAgent struct {
	ExternalID string
}
