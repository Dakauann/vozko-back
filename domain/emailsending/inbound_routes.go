package emailsending

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

var ErrInboundCapacityReached = errors.New("emailsending: every receipt rule is full, no reply domain can be added")

type ReplyRoute struct {
	ReplyDomain string
	Paused      bool
}

type InboundRoutes interface {
	EnsureReplyRoute(ctx context.Context, replyDomain string) error
	PauseReplies(ctx context.Context, replyDomain string) error
	ResumeReplies(ctx context.Context, replyDomain string) error
	RemoveReplyRoute(ctx context.Context, replyDomain string) error
	AdoptLegacyRoutes(ctx context.Context, routes []ReplyRoute) error
	BlockSender(ctx context.Context, ip netip.Addr, until time.Time) (bool, error)
	UnblockExpired(ctx context.Context, now time.Time) error
	IPFilterCount(ctx context.Context) (int, error)
}

type ConnectingIPReader interface {
	ConnectingIP(raw []byte) (netip.Addr, bool)
}

type ReplyHold struct {
	WorkspaceID   string
	BalanceMicros int64
}

type ReplyPauses interface {
	MarkRepliesPaused(ctx context.Context, workspaceID, domainID string, at time.Time) error
	ClearRepliesPaused(ctx context.Context, workspaceID, domainID string) error
	ListReplyHoldMismatches(ctx context.Context) ([]ReplyHold, error)
	ListAll(ctx context.Context) ([]SendingDomain, error)
}
