package ws

import (
	"context"
	"errors"
	"log"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	"vozko/domain/sip_trunk"
	dialer_infra "vozko/infra/dialer"
	calls_usecase "vozko/usecases/calls"
	dialer_usecase "vozko/usecases/dialer"
)

type DialerInboundExecutor struct {
	calls         dialer_domain.DialerCallRegistry
	endUseCase    dialer_domain.EndOutboundCallUseCase
	lifecycle     *dialer_usecase.OutboundCallLifecycleRunner
	trunks        sip_trunk.TrunkManager
	recordingPool *calls_usecase.RecordingUploadPool
	logger        *log.Logger
	// legDeath is the customer-hangup funnel: an inbound call an agent answered
	// can later be transferred, and the transfer must abort the instant the
	// caller's leg dies. Wired by the container from the transfer use case.
	legDeath func(workspaceID, callID string)
}

// SetTransferLegDeathHook wires the transfer engine's customer-hangup funnel
// into every call this executor attaches.
func (x *DialerInboundExecutor) SetTransferLegDeathHook(hook func(workspaceID, callID string)) {
	x.legDeath = hook
}

func NewDialerInboundExecutor(
	calls dialer_domain.DialerCallRegistry,
	endUseCase dialer_domain.EndOutboundCallUseCase,
	lifecycle *dialer_usecase.OutboundCallLifecycleRunner,
	trunks sip_trunk.TrunkManager,
	recordingPool *calls_usecase.RecordingUploadPool,
	logger *log.Logger,
) *DialerInboundExecutor {
	if logger == nil {
		logger = log.Default()
	}
	return &DialerInboundExecutor{
		calls:         calls,
		endUseCase:    endUseCase,
		lifecycle:     lifecycle,
		trunks:        trunks,
		recordingPool: recordingPool,
		logger:        logger,
	}
}

func (x *DialerInboundExecutor) AttachInboundCall(ctx context.Context, input dialer_domain.AttachInboundCallInput) (conversation.CRMCall, error) {
	target, ok := input.Session.(*dialerSession)
	if !ok || target == nil {
		return nil, errors.New("inbound executor: target is not a delivery dialer session")
	}
	// At accept time the target holds this offer's own ring reservation; only a
	// genuine attached call (ActiveCallID != "") blocks the attach. Attach below
	// consumes the reservation atomically.
	if target.ActiveCallID() != "" {
		return nil, dialer_domain.ErrTransferTargetBusy
	}
	if input.CallSession.MediaSession == nil {
		return nil, errors.New("inbound executor: answered call has no media session")
	}

	callID := input.CallSession.ID
	trunkID := input.CallSession.TrunkID
	call := dialer_infra.NewVoipPassthroughCall(
		callID,
		input.PhoneNumber,
		input.CallSession.MediaSession,
		func() error {
			if x.trunks == nil {
				return nil
			}
			hangupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return x.trunks.Hangup(hangupCtx, trunkID, callID)
		},
		x.logger,
	)

	if _, err := attachDialerCall(ctx, dialerCallAttachInput{
		Session:      target,
		Call:         call,
		Admission:    input.Admission,
		Phone:        input.PhoneNumber,
		RequestID:    input.OfferID,
		WorkspaceID:  input.WorkspaceID,
		OwnerUserID:  input.UserID,
		SIPTrunkID:   input.SIPTrunkID,
		StartedAt:    input.StartedAt,
		Direction:    cdr.DirectionInbound,
		CallRegistry: x.calls,
		EndUseCase:   x.endUseCase,
		Lifecycle:    x.lifecycle,
		Logger:       x.logger,
		OnCallGone:   x.legDeath,
	}); err != nil {
		return nil, err
	}

	call.Start()
	return call, nil
}

func (x *DialerInboundExecutor) AttachInboundCRMCall(ctx context.Context, input dialer_domain.AttachInboundCRMCallInput) error {
	target, ok := input.Session.(*dialerSession)
	if !ok || target == nil {
		return errors.New("inbound executor: target is not a delivery dialer session")
	}
	if input.Call == nil {
		return errors.New("inbound executor: call is nil")
	}
	// At accept time the target holds this offer's own ring reservation; only a
	// genuine attached call (ActiveCallID != "") blocks the attach. Attach below
	// consumes the reservation atomically.
	if target.ActiveCallID() != "" {
		return dialer_domain.ErrTransferTargetBusy
	}

	if _, err := attachDialerCall(ctx, dialerCallAttachInput{
		Session:       target,
		Call:          input.Call,
		Admission:     input.Admission,
		Phone:         input.PhoneNumber,
		RequestID:     input.OfferID,
		WorkspaceID:   input.WorkspaceID,
		OwnerUserID:   input.UserID,
		SIPTrunkID:    "",
		StartedAt:     input.StartedAt,
		Direction:     cdr.DirectionInbound,
		CallRegistry:  x.calls,
		EndUseCase:    x.endUseCase,
		Lifecycle:     x.lifecycle,
		RecordingPool: x.recordingPool,
		Logger:        x.logger,
		OnCallGone:    x.legDeath,
	}); err != nil {
		return err
	}

	if starter, ok := input.Call.(interface{ Start() }); ok {
		starter.Start()
	}
	return nil
}

var _ dialer_domain.DialerInboundExecutor = (*DialerInboundExecutor)(nil)
var _ dialer_domain.InboundCRMCallExecutor = (*DialerInboundExecutor)(nil)
