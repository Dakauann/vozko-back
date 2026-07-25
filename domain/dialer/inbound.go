package dialer

import (
	"context"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/sip_trunk"
)

const DialerControlInboundCall = "call:incoming"

type InboundCallOffer struct {
	OfferID     string `json:"offer_id"`
	CallID      string `json:"call_id"`
	WorkspaceID string `json:"workspace_id"`
	TrunkID     string `json:"trunk_id"`
	FromNumber  string `json:"from_number"`
	ToNumber    string `json:"to_number,omitempty"`

	Channel   string    `json:"channel,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type AcceptInboundCallInput struct {
	OfferID     string
	WorkspaceID string
	UserID      string
	SessionID   string
}

type DeclineInboundCallInput struct {
	OfferID     string
	WorkspaceID string
	UserID      string
	SessionID   string
	Reason      string
}

type AttachInboundCallInput struct {
	OfferID     string
	WorkspaceID string
	UserID      string
	Session     DialerSession
	PhoneNumber string
	SIPTrunkID  string
	CallSession sip_trunk.TrunkCallSession
	Admission   *CallAdmissionLease
	StartedAt   time.Time
}

type DialerInboundExecutor interface {
	AttachInboundCall(ctx context.Context, input AttachInboundCallInput) (conversation.CRMCall, error)
}

type AttachInboundCRMCallInput struct {
	OfferID     string
	WorkspaceID string
	UserID      string
	Session     DialerSession
	PhoneNumber string
	Call        conversation.CRMCall
	Admission   *CallAdmissionLease
	StartedAt   time.Time
}

type InboundCRMCallExecutor interface {
	AttachInboundCRMCall(ctx context.Context, input AttachInboundCRMCallInput) error
}

type InboundCallUseCase interface {
	HandleInboundInvite(ctx context.Context, invite sip_trunk.InboundInvite) error
	Accept(ctx context.Context, input AcceptInboundCallInput) error
	Decline(ctx context.Context, input DeclineInboundCallInput) error
}

// ReceptiveInboundHandler answers an inbound call with a receptive campaign's
// agent or workflow when one is armed for the trunk. It is consulted before the
// human-dialer roulette path: when it returns handled=true it has fully taken
// ownership of the dialog (answered/bridged/hung up, or rejected), and the
// roulette path must not run. handled=false means no receptive campaign applies
// and no SIP signaling was sent, so the caller proceeds with human routing.
//
// Implemented in usecases/calls (where the bridge + campaign/lead/entry repos
// live); declared here as an interface so the dialer package depends only on the
// abstraction and no import cycle forms.
type ReceptiveInboundHandler interface {
	TryHandleInbound(ctx context.Context, invite sip_trunk.InboundInvite) (handled bool, err error)
}
