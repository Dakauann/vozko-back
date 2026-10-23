package emailapi

import (
	"context"
	"time"

	es "vozko/domain/emailsending"
)

type ClaimResult struct {
	Send     Send
	Created  bool
	Messages []Message
}

type SendStore interface {
	Claim(ctx context.Context, send Send, messages []Message, tags []Tag) (ClaimResult, error)
	TakeLease(ctx context.Context, workspaceID, sendID string, now, until time.Time) (bool, error)
	ReleaseLease(ctx context.Context, sendID string) error
	Messages(ctx context.Context, sendID string) ([]Message, error)
	OpenRound(ctx context.Context, sendID string, round int, units, unitMicros, unitCostMicros int64, positions []int) error
	SettleRound(ctx context.Context, sendID string, round int) error
	MarkSent(ctx context.Context, sendID string, position int, messageID string, status MessageStatus) error
	MarkRefused(ctx context.Context, sendID string, positions []int, code string) error
	MarkStatus(ctx context.Context, messageID string, status MessageStatus, event string, at time.Time) (bool, error)
}

type LogReader interface {
	List(ctx context.Context, q LogQuery) ([]LogEntry, error)
	Get(ctx context.Context, workspaceID, id string) (*LogEntry, error)
	GetByMessageID(ctx context.Context, messageID string) (*LogEntry, error)
	Events(ctx context.Context, messageID string) ([]EventView, error)
}

type RunMessages = es.RunMessages
