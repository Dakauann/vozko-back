// Package agentctx carries the per-request agent context (the resolved agent and
// the tool execution tracker) through the tool-execution call chain.
package agentctx

import (
	"context"
	"sync"
	"time"

	agent_domain "vozko/domain/agent"
)

type agentContextKey struct{}
type toolExecutionTrackerContextKey struct{}

// ToolExecutionTracker debounces repeated tool invocations within a cooldown so a
// looping model cannot fire the same side effect twice in quick succession.
type ToolExecutionTracker struct {
	mu         sync.RWMutex
	executions map[string]time.Time
}

func NewToolExecutionTracker() *ToolExecutionTracker {
	return &ToolExecutionTracker{
		executions: make(map[string]time.Time),
	}
}

func (t *ToolExecutionTracker) CanExecute(toolName, uniqueKey string, cooldown time.Duration) bool {
	t.mu.RLock()
	key := toolName + ":" + uniqueKey
	lastExec, exists := t.executions[key]
	t.mu.RUnlock()

	if !exists {
		return true
	}
	return time.Since(lastExec) > cooldown
}

func (t *ToolExecutionTracker) MarkExecuted(toolName, uniqueKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := toolName + ":" + uniqueKey
	t.executions[key] = time.Now()
}

func WithToolExecutionTracker(ctx context.Context, tracker *ToolExecutionTracker) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, toolExecutionTrackerContextKey{}, tracker)
}

func ToolExecutionTrackerFromContext(ctx context.Context) (*ToolExecutionTracker, bool) {
	if ctx == nil {
		return nil, false
	}
	tracker, ok := ctx.Value(toolExecutionTrackerContextKey{}).(*ToolExecutionTracker)
	if !ok || tracker == nil {
		return nil, false
	}
	return tracker, true
}

func WithAgent(ctx context.Context, agent *agent_domain.Agent) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if agent == nil {
		return ctx
	}
	return context.WithValue(ctx, agentContextKey{}, agent)
}

func AgentFromContext(ctx context.Context) (*agent_domain.Agent, bool) {
	if ctx == nil {
		return nil, false
	}
	agent, ok := ctx.Value(agentContextKey{}).(*agent_domain.Agent)
	if !ok || agent == nil {
		return nil, false
	}
	return agent, true
}
