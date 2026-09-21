package ws

import (
	"context"
	"log"
	"time"

	cdr "vozko/domain/calls/cdr"
	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
	calls_usecase "vozko/usecases/calls"
	callsession_usecase "vozko/usecases/callsession"
)

type callAttachInput struct {
	Session       *callSession
	Call          conversation.CRMCall
	Admission     *callsession_domain.CallAdmissionLease
	Phone         string
	RequestID     string
	WorkspaceID   string
	OwnerUserID   string
	EntryID       string
	LeadID        string
	StartedAt     time.Time
	Direction     cdr.Direction
	CallRegistry  callsession_domain.CallRegistry
	EndUseCase    callsession_domain.EndOutboundCallUseCase
	Lifecycle     *callsession_usecase.OutboundCallLifecycleRunner
	RecordingPool *calls_usecase.RecordingUploadPool
	Logger        *log.Logger
}

func attachCall(ctx context.Context, input callAttachInput) (*liveCall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if input.Logger == nil {
		input.Logger = log.Default()
	}
	if input.Session == nil {
		return nil, errCallSessionBusy
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now()
	}

	if input.RecordingPool != nil && input.Call != nil && cdr.IsWhatsAppCallID(input.Call.ID()) {
		if rec := calls_usecase.NewRecordingCRMCall(input.Call, input.RecordingPool, input.WorkspaceID, input.EntryID, input.LeadID); rec != nil {
			input.Call = rec
		}
	}

	liveCall := &liveCall{
		call:          input.Call,
		admission:     input.Admission,
		phone:         input.Phone,
		requestID:     input.RequestID,
		workspaceID:   input.WorkspaceID,
		ownerUserID:   input.OwnerUserID,
		startedAt:     input.StartedAt,
		direction:     input.Direction,
		lifecycleDone: make(chan struct{}),
	}

	var unregisterCall func()
	if input.CallRegistry != nil && input.Call != nil {
		if err := input.CallRegistry.Register(callsession_domain.CallEntry{
			CallID:         input.Call.ID(),
			WorkspaceID:    input.WorkspaceID,
			OwnerSessionID: input.Session.ID(),
			OwnerUserID:    input.OwnerUserID,
			Phone:          input.Phone,
			Call:           input.Call,
			Lease:          input.Admission,
		}); err != nil {
			input.Logger.Printf("[CallSessionWS] call registry rejected entry: %v", err)
		} else {
			workspaceID := input.WorkspaceID
			callID := input.Call.ID()
			registry := input.CallRegistry
			unregisterCall = func() { registry.Unregister(workspaceID, callID) }
		}
	}

	if err := input.Session.Attach(liveCall); err != nil {
		if unregisterCall != nil {
			unregisterCall()
		}
		if input.EndUseCase != nil {
			_ = input.EndUseCase.Execute(ctx, callsession_domain.EndOutboundCallInput{
				Call:             input.Call,
				Admission:        input.Admission,
				Hangup:           true,
				ReleaseAdmission: true,
			})
		}
		return nil, err
	}

	liveCall.start(ctx, input.Lifecycle, input.EndUseCase, input.Logger, func() {
		if unregisterCall != nil {
			unregisterCall()
		}
	})
	return liveCall, nil
}
