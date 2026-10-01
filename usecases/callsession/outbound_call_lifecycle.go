package callsession_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/calls/billing"
	cdr "vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/messaging"
)

const (
	reservationLeadDefault  = 10 * time.Second
	reservationRetryDefault = 2 * time.Second
	inflightReservationTTL  = 5 * time.Minute
)

const (
	endReasonInsufficientBalance = "insufficient_balance"
	endReasonBalanceCheckError   = "balance_check_error"
)

type OutboundCallLifecycleInput struct {
	Call        conversation.CRMCall
	Admission   *callsession.CallAdmissionLease
	WorkspaceID string
	StartedAt   time.Time

	OwnerUserID string

	Direction cdr.Direction
	PhoneTo   string

	OnStatus func(event conversation.CallEvent)

	OnAudio func(pcm []byte)

	OnEnded func(reason string, duration time.Duration)
}

type OutboundCallLifecycleRunner struct {
	admission            callsession.CallAdmissionCoordinator
	cachedBalanceChecker balance.CachedBalanceChecker
	inflightReserver     balance.InflightReserver
	billingPub           messaging.MessageQueuePub
	cdrStart             cdr.StartCallUseCase
	cdrAnswered          cdr.MarkCallAnsweredUseCase
	cdrComplete          cdr.CompleteCallUseCase
	billingMinute        time.Duration
	reservationLead      time.Duration
	reservationRetry     time.Duration
	logger               *log.Logger
	nowFn                func() time.Time
}

func (r *OutboundCallLifecycleRunner) SetCDRStart(uc cdr.StartCallUseCase) {
	if r != nil {
		r.cdrStart = uc
	}
}

func (r *OutboundCallLifecycleRunner) SetCDRAnswered(uc cdr.MarkCallAnsweredUseCase) {
	if r != nil {
		r.cdrAnswered = uc
	}
}

func (r *OutboundCallLifecycleRunner) SetCDRComplete(uc cdr.CompleteCallUseCase) {
	if r != nil {
		r.cdrComplete = uc
	}
}

func NewOutboundCallLifecycleRunner(
	admission callsession.CallAdmissionCoordinator,
	cachedBalanceChecker balance.CachedBalanceChecker,
	inflightReserver balance.InflightReserver,
	billingPub messaging.MessageQueuePub,
	logger *log.Logger,
) (*OutboundCallLifecycleRunner, error) {
	if billingPub == nil {
		return nil, fmt.Errorf("%w: outbound call lifecycle", callsession.ErrBillingNotConfigured)
	}
	if logger == nil {
		logger = log.Default()
	}
	return &OutboundCallLifecycleRunner{
		admission:            admission,
		cachedBalanceChecker: cachedBalanceChecker,
		inflightReserver:     inflightReserver,
		billingPub:           billingPub,
		billingMinute:        billing.BillingMinute,
		reservationLead:      reservationLeadDefault,
		reservationRetry:     reservationRetryDefault,
		logger:               logger,
		nowFn:                time.Now,
	}, nil
}

func (r *OutboundCallLifecycleRunner) SetMinuteWaves(minute, lead, retry time.Duration) {
	if minute > 0 && lead >= 0 && retry > 0 {
		r.billingMinute, r.reservationLead, r.reservationRetry = minute, lead, retry
	}
}

func (r *OutboundCallLifecycleRunner) SetNowFn(fn func() time.Time) {
	if fn != nil {
		r.nowFn = fn
	}
}

func (r *OutboundCallLifecycleRunner) Run(ctx context.Context, input OutboundCallLifecycleInput) {
	if r == nil {
		return
	}

	if input.Call == nil {
		if input.Admission != nil && r.admission != nil {
			if err := r.admission.Release(input.Admission); err != nil {
				r.logger.Printf("[CallLifecycle] admission release error for ws %s (nil call): %v", input.Admission.WorkspaceID, err)
			}
		}
		return
	}

	callRecordID := r.startCDR(input)

	var perMinCost, admittedMicros int64
	if input.Admission != nil {
		perMinCost = input.Admission.PerMinuteCostMicros
		admittedMicros = input.Admission.ReservedMicros
	}
	guard := r.newMinuteGuard(input.WorkspaceID, input.Call.ID(), perMinCost, admittedMicros)
	var answeredAt time.Time
	endReason := string(conversation.CallEventEnded)

	defer func() {
		guard.close()
		r.releaseAdmission(input.Admission, guard.reservedMicros)
		r.publishBilling(input, callRecordID, answeredAt)
		r.completeCDR(input.Call.ID(), answeredAt, endReason)
	}()

	call := input.Call
	audioCh := call.AudioStream()
	events := call.Events()

	sentEnded := false
	emitEnded := func(reason string) {
		if sentEnded {
			return
		}
		sentEnded = true
		endReason = reason
		if input.OnEnded != nil {
			var talk time.Duration
			if !answeredAt.IsZero() {
				talk = r.nowFn().Sub(answeredAt)
			}
			input.OnEnded(reason, talk)
		}
	}

	for {
		select {
		case <-ctx.Done():
			emitEnded("cancelled")
			return
		case <-call.Done():
			emitEnded(pendingTerminalReason(events))
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				emitEnded("ended")
				return
			}
			if ev.Type == conversation.CallEventAnswered && answeredAt.IsZero() {
				answeredAt = r.nowFn()
				r.markAnswered(input.Call.ID())
				guard.answered(answeredAt)
			}
			if ev.IsTerminal() {
				emitEnded(string(ev.Type))
				return
			}
			if input.OnStatus != nil {
				input.OnStatus(ev)
			}
		case pcm, ok := <-audioCh:
			if !ok {
				audioCh = nil
				continue
			}
			if input.OnAudio != nil {
				input.OnAudio(pcm)
			}
		case <-guard.nextC():
			guard.reserveNextMinute()
		case <-guard.stopC():
			emitEnded(guard.failure)
			go func() { _ = call.Hangup() }()
			return
		}
	}
}

func (r *OutboundCallLifecycleRunner) completeCDR(callID string, answeredAt time.Time, reason string) {
	if r.cdrComplete == nil {
		return
	}
	err := r.cdrComplete.Execute(cdr.CompleteCallInput{
		CallID:    callID,
		Status:    cdr.CompletionStatus(!answeredAt.IsZero(), reason),
		EndedAt:   r.nowFn(),
		EndReason: &reason,
	})
	if err != nil {
		r.logger.Printf("[CallCDR] failed to complete CDR for call %s: %v", callID, err)
	}
}

func (r *OutboundCallLifecycleRunner) releaseAdmission(lease *callsession.CallAdmissionLease, totalReservation int64) {
	if lease == nil {
		return
	}

	lease.ReservedMicros = totalReservation
	if r.admission != nil {
		if err := r.admission.Release(lease); err != nil {
			r.logger.Printf("[CallLifecycle] admission release error for ws %s: %v", lease.WorkspaceID, err)
		}
		return
	}

	if r.inflightReserver != nil && totalReservation > 0 && lease.WorkspaceID != "" {
		_ = r.inflightReserver.Release(lease.WorkspaceID, totalReservation)
	}
}

func (r *OutboundCallLifecycleRunner) startCDR(input OutboundCallLifecycleInput) string {
	if r == nil || r.cdrStart == nil || input.Call == nil {
		return ""
	}
	direction := input.Direction
	if direction == "" {
		direction = cdr.DirectionOutbound
	}

	callType := cdr.CallTypeCRM
	phoneFrom, phoneTo := "", input.PhoneTo
	if direction == cdr.DirectionInbound {
		phoneFrom, phoneTo = input.PhoneTo, ""
	}
	var agentIDPtr *string
	if uid := strings.TrimSpace(input.OwnerUserID); uid != "" {
		agentIDPtr = &uid
	}
	rec, err := r.cdrStart.Execute(cdr.StartCallInput{
		CallID:      input.Call.ID(),
		WorkspaceID: input.WorkspaceID,
		Type:        callType,
		Direction:   direction,
		Source:      cdr.SourceForCallID(input.Call.ID()),
		AgentID:     agentIDPtr,
		PhoneFrom:   phoneFrom,
		PhoneTo:     phoneTo,
		StartedAt:   input.StartedAt,
	})
	if err != nil {
		r.logger.Printf("[CallCDR] failed to start CDR for call %s (ws %s): %v", input.Call.ID(), input.WorkspaceID, err)
		return ""
	}
	if rec == nil {
		return ""
	}
	return rec.ID
}

func (r *OutboundCallLifecycleRunner) markAnswered(callID string) {
	if r == nil || r.cdrAnswered == nil || callID == "" {
		return
	}
	if err := r.cdrAnswered.Execute(callID, r.nowFn()); err != nil {
		r.logger.Printf("[CallCDR] failed to mark answered for call %s: %v", callID, err)
	}
}

func (r *OutboundCallLifecycleRunner) publishBilling(input OutboundCallLifecycleInput, callRecordID string, answeredAt time.Time) {
	if r.billingPub == nil || input.Call == nil {
		return
	}
	if answeredAt.IsZero() {
		r.logger.Printf("[CallBilling] call %s was never answered, nothing to bill", input.Call.ID())
		return
	}
	callEnd := r.nowFn()
	channel := ""
	if input.Admission != nil {
		channel = input.Admission.CallChannel
	}
	event := billing.CallCompletedEvent{
		CallID:       input.Call.ID(),
		WorkspaceID:  input.WorkspaceID,
		CallSource:   billing.CallSourceWebSocket,
		Channel:      channel,
		CallStart:    answeredAt,
		CallEnd:      callEnd,
		DurationSec:  billing.BillableSeconds(answeredAt, callEnd),
		CallRecordID: callRecordID,
	}
	data, err := json.Marshal(event)
	if err != nil {
		r.logger.Printf("[CallBilling] failed to marshal billing event for call %s: %v", event.CallID, err)
		return
	}
	if err := r.billingPub.Publish(billing.TopicCallCompleted, data); err != nil {
		r.logger.Printf("[CallBilling] failed to publish billing event for call %s: %v", event.CallID, err)
		return
	}
	r.logger.Printf("[CallBilling] published billing event for call %s (billable=%ds, channel=%s, workspace=%s)",
		event.CallID, event.DurationSec, channel, input.WorkspaceID)
}

func pendingTerminalReason(events <-chan conversation.CallEvent) string {
	if outcome, ok := conversation.PendingOutcome(events); ok {
		return string(outcome.Type)
	}
	return string(conversation.CallEventEnded)
}
