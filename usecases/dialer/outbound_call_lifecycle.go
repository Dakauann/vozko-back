package dialer_usecase

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/calls/billing"
	cdr "vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	"vozko/domain/dialer"
	"vozko/domain/messaging"
)

const balanceGuardDefaultInterval = 30 * time.Second

const balanceGuardFailClosedThreshold = 3

type OutboundCallLifecycleInput struct {
	Call        conversation.CRMCall
	Admission   *dialer.CallAdmissionLease
	WorkspaceID string
	StartedAt   time.Time

	// OwnerUserID is the dialer human (stored as calls.agent_id for member metrics).
	OwnerUserID string

	Direction cdr.Direction
	PhoneTo   string
	TrunkID   string

	OnStatus func(event conversation.CallEvent)

	OnAudio func(pcm []byte)

	OnEnded func(reason string, duration time.Duration)
}

type OutboundCallLifecycleRunner struct {
	admission            dialer.CallAdmissionCoordinator
	cachedBalanceChecker balance.CachedBalanceChecker
	inflightReserver     balance.InflightReserver
	billingPub           messaging.MessageQueuePub
	cdrStart             cdr.StartCallUseCase
	cdrAnswered          cdr.MarkCallAnsweredUseCase
	balanceGuardInterval time.Duration
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

func NewOutboundCallLifecycleRunner(
	admission dialer.CallAdmissionCoordinator,
	cachedBalanceChecker balance.CachedBalanceChecker,
	inflightReserver balance.InflightReserver,
	billingPub messaging.MessageQueuePub,
	logger *log.Logger,
) *OutboundCallLifecycleRunner {
	if logger == nil {
		logger = log.Default()
	}
	return &OutboundCallLifecycleRunner{
		admission:            admission,
		cachedBalanceChecker: cachedBalanceChecker,
		inflightReserver:     inflightReserver,
		billingPub:           billingPub,
		balanceGuardInterval: balanceGuardDefaultInterval,
		logger:               logger,
		nowFn:                time.Now,
	}
}

func (r *OutboundCallLifecycleRunner) SetBalanceGuardInterval(d time.Duration) {
	if d > 0 {
		r.balanceGuardInterval = d
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
				r.logger.Printf("[DialerLifecycle] admission release error for ws %s (nil call): %v", input.Admission.WorkspaceID, err)
			}
		}
		return
	}

	callRecordID := r.startCDR(input)

	var (
		perMinCost    int64
		myReservation int64
	)
	if input.Admission != nil {
		perMinCost = input.Admission.PerMinuteCostMicros
		myReservation = input.Admission.ReservedMicros
	}
	balanceErrors := 0

	defer func() {
		r.releaseAdmission(input.Admission, myReservation)
		r.publishBilling(input, callRecordID)
	}()

	call := input.Call
	audioCh := call.AudioStream()
	events := call.Events()

	var tickerC <-chan time.Time
	if perMinCost > 0 && r.inflightReserver != nil && r.cachedBalanceChecker != nil {
		t := time.NewTicker(r.balanceGuardInterval)
		defer t.Stop()
		tickerC = t.C
	}

	sentEnded := false
	emitEnded := func(reason string) {
		if sentEnded {
			return
		}
		sentEnded = true
		if input.OnEnded != nil {
			input.OnEnded(reason, r.nowFn().Sub(input.StartedAt))
		}
	}

	for {
		select {
		case <-ctx.Done():
			emitEnded("cancelled")
			return
		case <-call.Done():
			emitEnded("ended")
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				emitEnded("ended")
				return
			}
			if ev.Type == conversation.CallEventAnswered {
				r.markAnswered(input.Call.ID())
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
		case <-tickerC:
			newReservation, abortReason, abort := r.extendReservation(input, perMinCost, myReservation, &balanceErrors)
			if abort {
				emitEnded(abortReason)
				go func() { _ = call.Hangup() }()
				return
			}
			myReservation = newReservation
		}
	}
}

func (r *OutboundCallLifecycleRunner) extendReservation(
	input OutboundCallLifecycleInput,
	perMinCost int64,
	current int64,
	balanceErrors *int,
) (int64, string, bool) {
	elapsed := r.nowFn().Sub(input.StartedAt)
	projectedEnd := elapsed + r.balanceGuardInterval

	projectedSec := int64(projectedEnd.Seconds())
	projectedMinutes := (projectedSec + 59) / 60
	projectedCost := projectedMinutes * perMinCost
	delta := projectedCost - current
	if delta <= 0 {
		return current, "", false
	}

	budget, err := r.cachedBalanceChecker.GetBalance(input.WorkspaceID)
	if err != nil {
		*balanceErrors++
		r.logger.Printf("[DialerBalanceGuard] balance read error for ws %s: %v (consecutive: %d)",
			input.WorkspaceID, err, *balanceErrors)
		if *balanceErrors >= balanceGuardFailClosedThreshold {
			r.logger.Printf("[DialerBalanceGuard] CRITICAL: %d consecutive balance errors for ws %s — terminating call (fail-closed)",
				*balanceErrors, input.WorkspaceID)
			return current, "balance_check_error", true
		}
		return current, "", false
	}

	ok, err := r.inflightReserver.Reserve(input.WorkspaceID, delta, budget)
	if err != nil {
		*balanceErrors++
		r.logger.Printf("[DialerBalanceGuard] reserve error for ws %s: %v (consecutive: %d)",
			input.WorkspaceID, err, *balanceErrors)
		if *balanceErrors >= balanceGuardFailClosedThreshold {
			r.logger.Printf("[DialerBalanceGuard] CRITICAL: %d consecutive reserve errors for ws %s — terminating call (fail-closed)",
				*balanceErrors, input.WorkspaceID)
			return current, "balance_check_error", true
		}
		return current, "", false
	}
	*balanceErrors = 0

	if !ok {
		r.logger.Printf("[DialerBalanceGuard] insufficient balance for ws %s (budget=%d) — ending call %s",
			input.WorkspaceID, budget, input.Call.ID())
		return current, "insufficient_balance", true
	}

	newReservation := current + delta
	_ = r.inflightReserver.RefreshTTL(input.WorkspaceID, 5*time.Minute)
	return newReservation, "", false
}

func (r *OutboundCallLifecycleRunner) releaseAdmission(lease *dialer.CallAdmissionLease, totalReservation int64) {
	if lease == nil {
		return
	}

	lease.ReservedMicros = totalReservation
	if r.admission != nil {
		if err := r.admission.Release(lease); err != nil {
			r.logger.Printf("[DialerLifecycle] admission release error for ws %s: %v", lease.WorkspaceID, err)
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

	isWhatsApp := cdr.IsWhatsAppCallID(input.Call.ID())
	callType := cdr.CallTypeCRM
	if direction == cdr.DirectionInbound && !isWhatsApp {
		callType = cdr.CallTypeTrunkInbound
	}
	var trunkIDPtr *string
	if input.TrunkID != "" {
		t := input.TrunkID
		trunkIDPtr = &t
	}
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
		TrunkID:     trunkIDPtr,
		StartedAt:   input.StartedAt,
	})
	if err != nil {
		r.logger.Printf("[DialerCDR] failed to start CDR for call %s (ws %s): %v", input.Call.ID(), input.WorkspaceID, err)
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
		r.logger.Printf("[DialerCDR] failed to mark answered for call %s: %v", callID, err)
	}
}

func (r *OutboundCallLifecycleRunner) publishBilling(input OutboundCallLifecycleInput, callRecordID string) {
	if r.billingPub == nil || input.Call == nil {
		return
	}
	callEnd := r.nowFn()
	durationSec := int(callEnd.Sub(input.StartedAt).Seconds())
	if durationSec <= 0 {
		return
	}
	event := billing.CallCompletedEvent{
		CallID:       input.Call.ID(),
		WorkspaceID:  input.WorkspaceID,
		CallSource:   billing.CallSourceWebSocket,
		CallStart:    input.StartedAt,
		CallEnd:      callEnd,
		DurationSec:  durationSec,
		CallRecordID: callRecordID,
	}
	data, err := json.Marshal(event)
	if err != nil {
		r.logger.Printf("[DialerBilling] failed to marshal billing event for call %s: %v", event.CallID, err)
		return
	}
	if err := r.billingPub.Publish(billing.TopicCallCompleted, data); err != nil {
		r.logger.Printf("[DialerBilling] failed to publish billing event for call %s: %v", event.CallID, err)
		return
	}
	r.logger.Printf("[DialerBilling] published billing event for call %s (duration=%ds, workspace=%s)",
		event.CallID, durationSec, input.WorkspaceID)
}
