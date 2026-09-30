package callrouting_usecase

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/callrouting"
	"vozko/domain/callsession"
	callsession_usecase "vozko/usecases/callsession"
)

const directTransferRingTimeout = 20 * time.Second

type WorkspaceHoldMusic interface {
	HoldMusicFor(ctx context.Context, workspaceID string) callrouting.HoldMusicRef
}

type TransferDeps struct {
	Calls      callrouting.CallDirectory
	Queues     callrouting.QueueRepository
	Dispatcher *Dispatcher
	Ringer     *callsession_usecase.InboundRinger
	Music      callrouting.HoldMusicLibrary
	HoldMusic  WorkspaceHoldMusic
	Log        callrouting.TransferLog
	Names      callrouting.MemberNames
	Logger     *log.Logger

	ReconnectGrace time.Duration
}

type TransferInput struct {
	WorkspaceID string
	UserID      string
	CallID      string
	Target      callrouting.TransferTarget
	Notes       string
}

type TransferCall struct {
	deps           TransferDeps
	ringTimeout    time.Duration
	reconnectGrace time.Duration
	now            func() time.Time

	mu      sync.Mutex
	pending map[string]*pendingTransfer
}

type pendingTransfer struct {
	fromUserID string
	cancel     context.CancelFunc
	cancelled  bool
}

type transferRun struct {
	record  callrouting.TransferRecord
	call    callrouting.RoutedCall
	owner   callsession.CallSession
	notify  func(callsession.TransferStatus)
	context callsession.TransferContext
}

func NewTransferCall(deps TransferDeps) *TransferCall {
	if deps.Logger == nil {
		deps.Logger = log.Default()
	}
	grace := deps.ReconnectGrace
	if grace <= 0 {
		grace = ownerReconnectGrace
	}
	return &TransferCall{deps: deps, ringTimeout: directTransferRingTimeout, reconnectGrace: grace, now: time.Now, pending: map[string]*pendingTransfer{}}
}

func (uc *TransferCall) Transfer(ctx context.Context, input TransferInput) (string, error) {
	notes, err := callrouting.NormalizeTransferNotes(input.Notes)
	if err != nil {
		return "", err
	}
	if err := input.Target.Validate(); err != nil {
		return "", err
	}
	call, ok := uc.deps.Calls.Find(input.WorkspaceID, input.CallID)
	if !ok {
		return "", callrouting.ErrCallNotFound
	}
	owner, owned := call.OwnerSession()
	if !owned || owner.UserID() != input.UserID {
		return "", callrouting.ErrNotCallOwner
	}
	if !uc.deps.Dispatcher.mayAnswer(input.UserID, input.WorkspaceID, call.Channel()) {
		return "", callrouting.ErrTransferNotAllowed
	}

	var queue *callrouting.Queue
	var target callsession.CallSession
	switch input.Target.Kind {
	case callrouting.TargetQueue:
		if queue, err = uc.deps.Queues.FindInWorkspace(ctx, input.WorkspaceID, input.Target.QueueID); err != nil {
			return "", err
		}
	case callrouting.TargetMember:
		if input.Target.UserID == input.UserID {
			return "", callrouting.ErrTransferToSelf
		}
		if target = uc.deps.Dispatcher.answerers(input.WorkspaceID, call.Channel())[input.Target.UserID]; target == nil {
			return "", callrouting.ErrTargetUnavailable
		}
	}

	ringCtx, err := uc.begin(input.CallID, input.UserID)
	if err != nil {
		return "", err
	}
	run := &transferRun{
		record:  uc.newRecord(input.WorkspaceID, input.CallID, input.UserID, input.Target, notes),
		call:    call,
		owner:   owner,
		context: callsession.TransferContext{FromUserID: input.UserID, FromName: uc.nameOf(input.UserID), Notes: notes},
	}
	run.notify = func(status callsession.TransferStatus) {
		status.TransferID, status.CallID = run.record.ID, input.CallID
		_ = owner.Notify(callsession.CallSessionControlMessage{Type: callsession.CallSessionTransferStatus, Payload: status})
	}
	if err := uc.deps.Log.Record(ctx, run.record); err != nil {
		uc.end(input.CallID)
		return "", err
	}

	if queue != nil {
		go uc.toQueue(ringCtx, run, queue)
	} else {
		go uc.toMember(ringCtx, run, target)
	}
	return run.record.ID, nil
}

func (uc *TransferCall) Cancel(workspaceID, userID, callID string) error {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	pending, ok := uc.pending[callID]
	if !ok || pending.fromUserID != userID {
		return callrouting.ErrNoTransferToCancel
	}
	if _, found := uc.deps.Calls.Find(workspaceID, callID); !found {
		return callrouting.ErrNoTransferToCancel
	}
	pending.cancelled = true
	pending.cancel()
	return nil
}

func (uc *TransferCall) toMember(ctx context.Context, run *transferRun, target callsession.CallSession) {
	defer uc.end(run.record.CallID)
	music, err := uc.holdMusic(ctx, run.record.WorkspaceID)
	if err == nil {
		err = run.call.Hold(music)
	}
	if err != nil {
		uc.deps.Logger.Printf("[CallTransfer] %s could not hold call %s: %v", run.record.ID, run.record.CallID, err)
		uc.finish(run.record, callrouting.OutcomeCancelled, "")
		run.notify(callsession.TransferStatus{Status: callsession.TransferStatusReturned, Reason: "hold_failed"})
		return
	}
	run.owner.Reserve(run.record.ID)
	run.notify(callsession.TransferStatus{Status: callsession.TransferStatusRinging, TargetUserID: target.UserID(), TargetName: uc.nameOf(target.UserID())})

	outcome := uc.deps.Ringer.Ring(ctx, callsession_usecase.RingRequest{
		Offer:        uc.offer(run, ""),
		Candidates:   []callsession.CallSession{target},
		PerCandidate: uc.ringTimeout,
		Deadline:     uc.now().Add(uc.ringTimeout),
		CallerGone:   run.call.Done(),
	})
	run.owner.Release(run.record.ID)

	if outcome.Session != nil {
		defer outcome.Session.Release(outcome.OfferID)
		if err := run.call.Connect(outcome.Session); err == nil {
			uc.finish(run.record, callrouting.OutcomeConnected, outcome.Session.UserID())
			run.notify(callsession.TransferStatus{Status: callsession.TransferStatusConnected, TargetUserID: outcome.Session.UserID()})
			return
		}
	}
	if callerLeft(run.call) {
		uc.finish(run.record, callrouting.OutcomeAbandoned, "")
		run.notify(callsession.TransferStatus{Status: callsession.TransferStatusEnded, Reason: "caller_hung_up"})
		return
	}
	reason, outcomeKind := "no_answer", callrouting.OutcomeReturned
	switch {
	case uc.wasCancelled(run.record.CallID):
		reason, outcomeKind = "cancelled", callrouting.OutcomeCancelled
	case outcome.DeclinedByUserID != "":
		reason = "declined"
	}
	uc.returnToOwner(run, outcomeKind, reason)
}

func (uc *TransferCall) toQueue(ctx context.Context, run *transferRun, queue *callrouting.Queue) {
	defer uc.end(run.record.CallID)
	run.notify(callsession.TransferStatus{Status: callsession.TransferStatusQueued, QueueID: queue.ID, QueueName: queue.Name})
	result, err := uc.deps.Dispatcher.Enqueue(ctx, EnqueueInput{
		Queue:      queue,
		Call:       run.call,
		Notes:      run.context.Notes,
		FromUserID: run.context.FromUserID,
		FromName:   run.context.FromName,
	})
	switch {
	case err == nil && result.Outcome == callrouting.OutcomeConnected:
		uc.finish(run.record, callrouting.OutcomeConnected, result.AgentUserID)
	case err == nil && result.Outcome == callrouting.OutcomeAbandoned:
		uc.finish(run.record, callrouting.OutcomeAbandoned, "")
	default:
		if err != nil {
			uc.deps.Logger.Printf("[CallTransfer] %s queue %s failed for call %s: %v", run.record.ID, queue.ID, run.record.CallID, err)
		}
		uc.ringBack(run, queue)
	}
}

func (uc *TransferCall) ringBack(run *transferRun, queue *callrouting.Queue) {
	session := uc.deps.Dispatcher.answerers(run.record.WorkspaceID, run.call.Channel())[run.record.FromUserID]
	if session != nil {
		outcome := uc.deps.Ringer.Ring(context.Background(), callsession_usecase.RingRequest{
			Offer:        uc.offer(run, queue.Name),
			Candidates:   []callsession.CallSession{session},
			PerCandidate: uc.ringTimeout,
			Deadline:     uc.now().Add(uc.ringTimeout),
			CallerGone:   run.call.Done(),
		})
		if outcome.Session != nil {
			defer outcome.Session.Release(outcome.OfferID)
			if err := run.call.Connect(outcome.Session); err == nil {
				uc.finish(run.record, callrouting.OutcomeReturned, outcome.Session.UserID())
				return
			}
		}
	}
	_ = run.call.Hangup()
	uc.finish(run.record, callrouting.OutcomeTimedOut, "")
}

func (uc *TransferCall) returnToOwner(run *transferRun, outcome callrouting.TransferOutcome, reason string) {
	if err := run.call.Connect(run.owner); err != nil {
		uc.deps.Logger.Printf("[CallTransfer] %s could not return call %s to %s, waiting for them to come back: %v", run.record.ID, run.record.CallID, run.record.FromUserID, err)
		if holdErr := uc.HoldForOwner(context.Background(), run.call, run.record.FromUserID); holdErr != nil {
			_ = run.call.Hangup()
			uc.finish(run.record, callrouting.OutcomeAbandoned, "")
			return
		}
	}
	uc.finish(run.record, outcome, run.record.FromUserID)
	run.notify(callsession.TransferStatus{Status: callsession.TransferStatusReturned, Reason: reason})
}

func (uc *TransferCall) offer(run *transferRun, queueName string) callsession.InboundCallOffer {
	transfer := run.context
	transfer.QueueName = queueName
	return callsession.InboundCallOffer{
		CallID:      run.call.ID(),
		WorkspaceID: run.record.WorkspaceID,
		FromNumber:  run.call.RemoteNumber(),
		Channel:     run.call.Channel(),
		Transfer:    &transfer,
	}
}

func (uc *TransferCall) holdMusic(ctx context.Context, workspaceID string) ([]byte, error) {
	ref := callrouting.HoldMusicRef{PresetID: callrouting.DefaultHoldPreset}
	if uc.deps.HoldMusic != nil {
		ref = uc.deps.HoldMusic.HoldMusicFor(ctx, workspaceID)
	}
	return playableMusic(ctx, uc.deps.Music, workspaceID, ref, uc.deps.Logger)
}

func (uc *TransferCall) nameOf(userID string) string {
	if uc.deps.Names == nil {
		return ""
	}
	return uc.deps.Names.ResolveUsernames([]string{userID})[userID]
}

func (uc *TransferCall) begin(callID, userID string) (context.Context, error) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if _, busy := uc.pending[callID]; busy {
		return nil, callrouting.ErrTransferInProgress
	}
	ctx, cancel := context.WithCancel(context.Background())
	uc.pending[callID] = &pendingTransfer{fromUserID: userID, cancel: cancel}
	return ctx, nil
}

func (uc *TransferCall) wasCancelled(callID string) bool {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	pending, ok := uc.pending[callID]
	return ok && pending.cancelled
}

func (uc *TransferCall) end(callID string) {
	uc.mu.Lock()
	pending, ok := uc.pending[callID]
	delete(uc.pending, callID)
	uc.mu.Unlock()
	if ok {
		pending.cancel()
	}
}

func (uc *TransferCall) finish(record callrouting.TransferRecord, outcome callrouting.TransferOutcome, answeredBy string) {
	if err := uc.deps.Log.Finish(context.Background(), record.WorkspaceID, record.ID, outcome, answeredBy, uc.now()); err != nil {
		uc.deps.Logger.Printf("[CallTransfer] %s could not record outcome %s: %v", record.ID, outcome, err)
	}
}

func (uc *TransferCall) newRecord(workspaceID, callID, fromUserID string, target callrouting.TransferTarget, notes string) callrouting.TransferRecord {
	return callrouting.TransferRecord{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		CallID:      callID,
		FromUserID:  fromUserID,
		Target:      target,
		Notes:       notes,
		Outcome:     callrouting.OutcomePending,
		CreatedAt:   uc.now(),
	}
}

func callerLeft(call callrouting.RoutedCall) bool {
	select {
	case <-call.Done():
		return true
	default:
		return false
	}
}
