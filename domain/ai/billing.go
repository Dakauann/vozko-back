package ai

const TopicAIBillingCompleted = "ai.billing.completed"

func RequestIDUnder(reference, unique string) string {
	if reference == "" {
		return unique
	}
	return reference + ":" + unique
}

func ReferencePrefix(reference string) string {
	return reference + ":"
}

type CallUsage struct {
	PromptTokens       int
	CompletionTokens   int
	CachedTokens       int
	CacheWriteTokens   int
	ReasoningTokens    int
	ProviderCostMicros int64
}

type AICompletedEvent struct {
	RequestID        string `json:"requestId"`
	WorkspaceID      string `json:"workspaceId"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	CachedTokens     int    `json:"cachedTokens,omitempty"`
	CacheWriteTokens int    `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  int    `json:"reasoningTokens,omitempty"`

	ProviderCostMicros int64 `json:"providerCostMicros,omitempty"`
}
