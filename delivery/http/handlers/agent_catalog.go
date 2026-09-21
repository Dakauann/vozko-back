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

func isSupportedMessagingModel(_ agent.AgentProvider, model string) bool {
	return strings.TrimSpace(model) != ""
}

const defaultMessagingModel = "openai/gpt-4o"
