package ai

const TopicAIBillingCompleted = "ai.billing.completed"

type AICompletedEvent struct {
	RequestID        string `json:"requestId"`
	WorkspaceID      string `json:"workspaceId"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`

	// ProviderCostMicros is what the provider says the call cost, in USD micros,
	// and it is the authoritative number when present.
	//
	// Deriving cost from tokens × a price table reproduces an invoice the
	// provider already computed, and only ever approximates it: OpenRouter also
	// charges for reasoning tokens, cache writes, images, audio, web search and
	// per-request fees, applies cache-read discounts, and reprices models
	// whenever an upstream does. Every one of those is inside this figure.
	//
	// Zero means "not reported" — a provider that does not return it, or an
	// older event still in the queue — and the token math stays as the fallback.
	// It is never a claim that the call was free.
	ProviderCostMicros int64 `json:"providerCostMicros,omitempty"`
}
