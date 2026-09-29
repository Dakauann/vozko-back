package sip_trunk

import (
	"context"
	"time"

	"vozko/domain/voip"
)

type CallDirection string

const (
	CallDirectionOutbound CallDirection = "outbound"
	CallDirectionInbound  CallDirection = "inbound"
)

type TrunkInviteInput struct {
	PhoneNumber string
}

type TrunkCallSession struct {
	ID          string
	TrunkID     string
	PhoneNumber string
	Direction   CallDirection
	StartedAt   time.Time
	AnsweredAt  time.Time
	LocalAddr   string
	RemoteAddr  string
	Audio       voip.PCMStream
	Media       voip.MediaInfo
}

type InboundDialog interface {
	ID() string
	FromUser() string
	ToUser() string
	Trying() error
	Ringing() error
	Answer(ctx context.Context) (TrunkCallSession, error)
	Hangup(ctx context.Context) error
	Done() <-chan struct{}
}

type InboundInvite struct {
	ID          string
	TrunkID     string
	WorkspaceID string
	FromNumber  string
	ToNumber    string
	ReceivedAt  time.Time
	Trunk       *SIPTrunk
	Dialog      InboundDialog
}

type InboundInviteHandler interface {
	HandleInboundInvite(ctx context.Context, invite InboundInvite) error
}
