package ws

import (
	"context"
	"errors"
	"log"

	cdr "vozko/domain/calls/cdr"
	callsession_domain "vozko/domain/callsession"
	calls_usecase "vozko/usecases/calls"
	callsession_usecase "vozko/usecases/callsession"
)

type CallSessionInboundExecutor struct {
	calls         callsession_domain.CallRegistry
	endUseCase    callsession_domain.EndOutboundCallUseCase
	lifecycle     *callsession_usecase.OutboundCallLifecycleRunner
	recordingPool *calls_usecase.RecordingUploadPool
	logger        *log.Logger
}

func NewCallSessionInboundExecutor(
	calls callsession_domain.CallRegistry,
	endUseCase callsession_domain.EndOutboundCallUseCase,
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner,
	recordingPool *calls_usecase.RecordingUploadPool,
	logger *log.Logger,
) *CallSessionInboundExecutor {
	if logger == nil {
		logger = log.Default()
	}
	return &CallSessionInboundExecutor{
		calls:         calls,
		endUseCase:    endUseCase,
		lifecycle:     lifecycle,
		recordingPool: recordingPool,
		logger:        logger,
	}
}

func (x *CallSessionInboundExecutor) AttachInboundCRMCall(ctx context.Context, input callsession_domain.AttachInboundCRMCallInput) error {
	target, ok := input.Session.(*callSession)
	if !ok || target == nil {
		return errors.New("inbound executor: target is not a delivery call session")
	}
	if input.Call == nil {
		return errors.New("inbound executor: call is nil")
	}
	// At accept time the target holds this offer's own ring reservation; only a
	// genuine attached call (ActiveCallID != "") blocks the attach. Attach below
	// consumes the reservation atomically.
	if target.ActiveCallID() != "" {
		return callsession_domain.ErrSessionBusy
	}

	if _, err := attachCall(ctx, callAttachInput{
		Session:       target,
		Call:          input.Call,
		Admission:     input.Admission,
		Phone:         input.PhoneNumber,
		RequestID:     input.OfferID,
		WorkspaceID:   input.WorkspaceID,
		OwnerUserID:   input.UserID,
		StartedAt:     input.StartedAt,
		Direction:     cdr.DirectionInbound,
		CallRegistry:  x.calls,
		EndUseCase:    x.endUseCase,
		Lifecycle:     x.lifecycle,
		RecordingPool: x.recordingPool,
		Logger:        x.logger,
	}); err != nil {
		return err
	}

	if starter, ok := input.Call.(interface{ Start() }); ok {
		starter.Start()
	}
	return nil
}

var _ callsession_domain.InboundCRMCallExecutor = (*CallSessionInboundExecutor)(nil)
