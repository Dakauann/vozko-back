package handlers

import (
	"strings"

	"vozko/brand"
	"vozko/domain/agent"
)

type agentProviderOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type agentProviderModels struct {
	LLM []string `json:"llm"`
}

func getSupportedAgentProviders() []agentProviderOption {
	return []agentProviderOption{
		{ID: string(agent.AgentProviderAI), Name: brand.Active().AIName},
	}
}

// Model validation is intentionally permissive: the messaging model catalog is
// loaded DYNAMICALLY (aiService.GetModelsWithPricing in ListOptions), so we
// accept any non-empty model id here instead of gating on a hardcoded list. The
// provider is kept in the signature for call-site symmetry / future use.
func isSupportedMessagingModel(_ agent.AgentProvider, model string) bool {
	return strings.TrimSpace(model) != ""
}

const defaultMessagingModel = "openai/gpt-4o"
