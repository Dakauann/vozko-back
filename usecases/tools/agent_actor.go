package tools_usecase

import (
	"context"
	"strings"

	"vozko/domain/actor"
	"vozko/usecases/agentctx"
)

func agentActor(ctx context.Context, config map[string]interface{}) string {
	agentID := agentIDOf(ctx, config)
	if agentID == "" {
		return ""
	}
	return actor.FormatAI(agentID)
}

func agentIDOf(ctx context.Context, config map[string]interface{}) string {
	if agentID := configString(config, "__agent_id"); agentID != "" {
		return agentID
	}
	if ctxAgent, ok := agentctx.AgentFromContext(ctx); ok {
		return strings.TrimSpace(ctxAgent.ID)
	}
	return ""
}

func automationActor(ctx context.Context, config map[string]interface{}) string {
	if agentID := agentActor(ctx, config); agentID != "" {
		return agentID
	}
	return actor.PlatformAI
}
