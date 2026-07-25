package ai

const TopicAIBillingCompleted = "ai.billing.completed"

type AICompletedEvent struct {
	RequestID        string `json:"requestId"`
	WorkspaceID      string `json:"workspaceId"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
}
