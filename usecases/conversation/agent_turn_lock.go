package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
)

const (
	agentTurnLockTTL  = 3 * time.Minute
	agentTurnLockPoll = 250 * time.Millisecond
)

var errAgentTurnLockUnwired = errors.New("agent turn lock: no shared state with a guarded delete wired")

type AgentTurnLock struct {
	state   cache.SharedState
	release cache.OwnedKeyDeleter
	ttl     time.Duration
	poll    time.Duration
}

func NewAgentTurnLock(state cache.SharedState) *AgentTurnLock {
	release, _ := state.(cache.OwnedKeyDeleter)
	return &AgentTurnLock{state: state, release: release, ttl: agentTurnLockTTL, poll: agentTurnLockPoll}
}

func agentTurnKey(entryID string, entryType shared.EntryType) string {
	return "agentturn:" + string(entryType) + ":" + entryID
}

func (l *AgentTurnLock) Acquire(ctx context.Context, entryID string, entryType shared.EntryType) (func(), error) {
	if l == nil || l.state == nil || l.release == nil {
		return func() {}, errAgentTurnLockUnwired
	}
	key := agentTurnKey(entryID, entryType)
	lock, err := cache.NewOwnedLock(l.state, l.ttl, l.poll)
	if err != nil {
		return func() {}, errAgentTurnLockUnwired
	}
	release, err := lock.Acquire(ctx, key)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return func() {}, err
		}
		return func() {}, fmt.Errorf("agent turn lock %s: %w", key, err)
	}
	return func() { reportAgentTurnRelease(key, release) }, nil
}

func reportAgentTurnRelease(key string, release func() (bool, error)) {
	released, err := release()
	switch {
	case err != nil:
		slog.Warn("conversation.agent_turn.unlock_failed", slog.String("key", key), slog.String("error", err.Error()))
	case !released:
		slog.Warn("conversation.agent_turn.lock_expired_before_release", slog.String("key", key))
	}
}

func (l *AgentTurnLock) HoldForInbound(ctx context.Context, entryID string, entryType shared.EntryType) (func(), bool) {
	if l == nil || entryID == "" {
		return func() {}, true
	}
	release, err := l.Acquire(ctx, entryID, entryType)
	attrs := []any{slog.String("entry_id", entryID), slog.String("entry_type", string(entryType))}
	switch {
	case err == nil:
		return release, true
	case ctx.Err() != nil:
		slog.Warn("conversation.agent_turn.wait_expired_not_replying", append(attrs, slog.String("error", err.Error()))...)
		return release, false
	}
	slog.Warn("conversation.agent_turn.lock_unavailable_replying_unlocked", append(attrs, slog.String("error", err.Error()))...)
	return release, true
}
