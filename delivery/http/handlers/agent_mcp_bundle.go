package handlers

type AgentMCPBundle struct {
	Builtin       *AgentMCPHandler
	Remote        *AgentMCPRemoteHandler
	OAuthCallback *AgentMCPOAuthCallbackHandler
	Collection    *AgentMCPCollectionHandler
}
