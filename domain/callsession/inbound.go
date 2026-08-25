package callsession

import (
	"context"
	"time"

	"vozko/domain/conversation"
)

const CallSessionInboundCall = "call:incoming"

type InboundCallOffer struct {
	OfferID     string `json:"offer_id"`
	CallID      string `json:"call_id"`
	WorkspaceID string `json:"workspace_id"`
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

type AttachInboundCRMCallInput struct {
	OfferID     string
	WorkspaceID string
	UserID      string
	Session     CallSession
	PhoneNumber string
	Call        conversation.CRMCall
	Admission   *CallAdmissionLease
	StartedAt   time.Time
}

type InboundCRMCallExecutor interface {
	AttachInboundCRMCall(ctx context.Context, input AttachInboundCRMCallInput) error
}

// InboundOfferResponder resolves an inbound call offer that is currently ringing
// an agent: the agent's browser answers or rejects it. Implemented by the
// inbound offer broker; declared here so delivery depends only on the port.
type InboundOfferResponder interface {
	Accept(ctx context.Context, input AcceptInboundCallInput) error
	Decline(ctx context.Context, input DeclineInboundCallInput) error
}
