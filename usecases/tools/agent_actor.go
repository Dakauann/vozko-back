package tools_usecase

import (
	"context"
	"strings"

	"vozko/domain/actor"
	"vozko/usecases/agentctx"
)

func agentActor(ctx context.Context, config map[string]interface{}) string {
	agentID := strings.TrimSpace(configString(config, "__agent_id"))
	if agentID == "" {
		if ctxAgent, ok := agentctx.AgentFromContext(ctx); ok {
			agentID = ctxAgent.ID
		}
	}
	if agentID == "" {
		return ""
	}
	return actor.FormatAI(agentID)
}
