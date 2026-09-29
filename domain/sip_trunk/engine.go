package sip_trunk

import (
	"context"
	"fmt"
	"time"
)

type ActiveCall struct {
	ID          string
	TrunkID     string
	Direction   CallDirection
	PhoneNumber string
	StartedAt   time.Time
	AnsweredAt  time.Time
}

type CallRejectedError struct {
	StatusCode int
	Reason     string
}

func (e *CallRejectedError) Error() string {
	return fmt.Sprintf("call rejected by the SIP provider: %d %s", e.StatusCode, e.Reason)
}

type Engine interface {
	RegisterTrunk(trunk *SIPTrunk) error
	RefreshTrunk(trunk *SIPTrunk) error
	UnregisterTrunk(trunkID string) error
	TrunkStatus(trunkID string) (SIPTrunkStatusUpdate, bool)
	Invite(ctx context.Context, trunkID string, input TrunkInviteInput) (TrunkCallSession, error)
	Hangup(ctx context.Context, trunkID string, callID string) error
	ActiveCalls(trunkID string) []ActiveCall
}
