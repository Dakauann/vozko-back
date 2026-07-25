package ws

import (
	"context"
	"log"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	calls_usecase "vozko/usecases/calls"
	dialer_usecase "vozko/usecases/dialer"
)

type dialerCallAttachInput struct {
	Session       *dialerSession
	Call          conversation.CRMCall
	Admission     *dialer_domain.CallAdmissionLease
	Phone         string
	RequestID     string
	WorkspaceID   string
	OwnerUserID   string
	SIPTrunkID    string
	EntryID       string
	LeadID        string
	StartedAt     time.Time
	Direction     cdr.Direction
	CallRegistry  dialer_domain.DialerCallRegistry
	EndUseCase    dialer_domain.EndOutboundCallUseCase
	Lifecycle     *dialer_usecase.OutboundCallLifecycleRunner
	RecordingPool *calls_usecase.RecordingUploadPool
	Logger        *log.Logger
	// OnCallGone fires when the call's lifecycle ends (after it is unregistered):
	// the customer-hangup funnel. A transfer in flight for this call is aborted
	// instantly instead of ghost-ringing the target until the reaper. Optional.
	OnCallGone func(workspaceID, callID string)
}

func attachDialerCall(ctx context.Context, input dialerCallAttachInput) (*liveDialerCall, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if input.Logger == nil {
		input.Logger = log.Default()
	}
	if input.Session == nil {
		return nil, errDialerSessionBusy
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now()
	}

	// WhatsApp calls are pion CRMCalls (PCM) with no voip.MediaSession, so they
	// can't be recorded at the RTP layer like SIP. Tap the channel-agnostic
	// CRMCall audio instead. SIP calls (any non-WhatsApp call ID) already record
	// at the RTP layer and must NOT be wrapped here, or they'd double-record.
	if input.RecordingPool != nil && input.Call != nil && cdr.IsWhatsAppCallID(input.Call.ID()) {
		if rec := calls_usecase.NewRecordingCRMCall(input.Call, input.RecordingPool, input.WorkspaceID, input.EntryID, input.LeadID); rec != nil {
			input.Call = rec
		}
	}

	liveCall := &liveDialerCall{
		call:          input.Call,
		admission:     input.Admission,
		phone:         input.Phone,
		requestID:     input.RequestID,
		workspaceID:   input.WorkspaceID,
		ownerUserID:   input.OwnerUserID,
		sipTrunkID:    input.SIPTrunkID,
		startedAt:     input.StartedAt,
		direction:     input.Direction,
		lifecycleDone: make(chan struct{}),
	}

	var unregisterCall func()
	if input.CallRegistry != nil && input.Call != nil {
		if err := input.CallRegistry.Register(dialer_domain.DialerCallEntry{
			CallID:         input.Call.ID(),
			WorkspaceID:    input.WorkspaceID,
			OwnerSessionID: input.Session.ID(),
			OwnerUserID:    input.OwnerUserID,
			SIPTrunkID:     input.SIPTrunkID,
			Phone:          input.Phone,
			Call:           input.Call,
			Lease:          input.Admission,
		}); err != nil {
			input.Logger.Printf("[DialerWS] call registry rejected entry: %v", err)
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
			_ = input.EndUseCase.Execute(ctx, dialer_domain.EndOutboundCallInput{
				Call:             input.Call,
				Admission:        input.Admission,
				Hangup:           true,
				ReleaseAdmission: true,
			})
		}
		return nil, err
	}

	onGone := input.OnCallGone
	goneWorkspaceID := input.WorkspaceID
	goneCallID := ""
	if input.Call != nil {
		goneCallID = input.Call.ID()
	}
	liveCall.start(ctx, input.Lifecycle, input.EndUseCase, input.Logger, func() {
		if unregisterCall != nil {
			unregisterCall()
		}
		if onGone != nil && goneCallID != "" {
			onGone(goneWorkspaceID, goneCallID)
		}
	})
	return liveCall, nil
}
