package dialer_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	dialer_domain "vozko/domain/dialer"
)

// Default knobs for the transfer engine's time-based behavior. They mirror the
// Asterisk features.conf semantics (see docs/CALL_TRANSFER_PRODUCTION_PLAN.md §4.11):
// OfferTTL ~ Dial timeout, RecallRetries ~ atxfercallbackretries, RecallDelay ~
// atxferloopdelay, MaxPark is the hard "never strand the caller" ceiling, and
// CompletingWedgeTimeout fails a completing handle whose executor never returned.
const (
	DefaultTransferOfferTTL      = 30 * time.Second
	DefaultRecallRetries         = 2
	DefaultRecallDelay           = 5 * time.Second
	DefaultRecallOfferWindow     = 25 * time.Second
	DefaultMaxPark               = 3 * time.Minute
	defaultCompletingWedgeExpiry = 30 * time.Second
)

// TransferTelemetry is an optional sink for CRM transfer timeline events (queue-backed).
// Implementations must not block or write Postgres on the call path.
type TransferTelemetry interface {
	OnTransferStage(workspaceID, callID, transferID, targetUserID, note string, stage dialer_domain.TransferStage)
}

type CallTransferUseCase struct {
	sessions dialer_domain.DialerSessionRegistry
	calls    dialer_domain.DialerCallRegistry
	store    dialer_domain.TransferStore
	executor dialer_domain.TransferExecutor
	logger   *log.Logger
	now      func() time.Time

	// queue is the OPTIONAL ACD director (the waiting-line policy brain). When nil,
	// no handle ever enters the queued stage and every behavior below is byte-for-byte
	// the pre-queue engine. When wired, a caller who cannot be placed immediately is
	// parked and queued instead of returned/hung-up, and the reaper Tick rings the
	// target's available pool in waves until the caller is connected or the queue
	// policy's bounds (MaxWait / overflow) terminate it.
	queue dialer_domain.QueueDirector

	// telemetry is OPTIONAL CRM timeline emission (never required for call correctness).
	telemetry TransferTelemetry

	offerTTL          time.Duration
	recallRetries     int
	recallDelay       time.Duration
	recallOfferWindow time.Duration
	maxPark           time.Duration
}

type CallTransferUseCaseConfig struct {
	Sessions dialer_domain.DialerSessionRegistry
	Calls    dialer_domain.DialerCallRegistry
	Store    dialer_domain.TransferStore
	Executor dialer_domain.TransferExecutor
	Logger   *log.Logger
	Now      func() time.Time

	// Queue is the OPTIONAL ACD director. Nil disables all queuing.
	Queue dialer_domain.QueueDirector

	// Telemetry is OPTIONAL CRM transfer timeline emission.
	Telemetry TransferTelemetry

	// Optional overrides; zero values take the defaults above.
	OfferTTL          time.Duration
	RecallRetries     int
	RecallDelay       time.Duration
	RecallOfferWindow time.Duration
	MaxPark           time.Duration
}

func NewCallTransferUseCase(cfg CallTransferUseCaseConfig) (*CallTransferUseCase, error) {
	if cfg.Sessions == nil {
		return nil, errors.New("transfer use case: sessions registry is required")
	}
	if cfg.Calls == nil {
		return nil, errors.New("transfer use case: call registry is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("transfer use case: store is required")
	}
	if cfg.Executor == nil {
		return nil, errors.New("transfer use case: executor is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	uc := &CallTransferUseCase{
		sessions:          cfg.Sessions,
		calls:             cfg.Calls,
		store:             cfg.Store,
		executor:          cfg.Executor,
		logger:            logger,
		now:               now,
		queue:             cfg.Queue,
		telemetry:         cfg.Telemetry,
		offerTTL:          cfg.OfferTTL,
		recallRetries:     cfg.RecallRetries,
		recallDelay:       cfg.RecallDelay,
		recallOfferWindow: cfg.RecallOfferWindow,
		maxPark:           cfg.MaxPark,
	}
	if uc.offerTTL <= 0 {
		uc.offerTTL = DefaultTransferOfferTTL
	}
	if uc.recallRetries <= 0 {
		uc.recallRetries = DefaultRecallRetries
	}
	if uc.recallDelay <= 0 {
		uc.recallDelay = DefaultRecallDelay
	}
	if uc.recallOfferWindow <= 0 {
		uc.recallOfferWindow = DefaultRecallOfferWindow
	}
	if uc.maxPark <= 0 {
		uc.maxPark = DefaultMaxPark
	}
	return uc, nil
}

var _ dialer_domain.CallTransferUseCase = (*CallTransferUseCase)(nil)

func (uc *CallTransferUseCase) Initiate(ctx context.Context, req dialer_domain.TransferRequest) (*dialer_domain.TransferHandle, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	entry, ok := uc.calls.Lookup(req.WorkspaceID, req.CallID)
	if !ok {
		return nil, dialer_domain.ErrTransferCallNotFound
	}
	if entry.OwnerUserID != req.InitiatorID {
		return nil, dialer_domain.ErrTransferNotOwner
	}

	// Fork the offer to EVERY live contact of the target member (browser softphone
	// and registered branch ring together, exactly like SIP forking to multiple
	// contacts of one AOR). This is the standard UCaaS behaviour (RingCentral/Zoom).
	targetSessions := uc.sessions.FindSessionsByUser(req.WorkspaceID, req.TargetUserID)
	if len(targetSessions) == 0 {
		return nil, dialer_domain.ErrTransferTargetOffline
	}

	now := uc.now()
	const human = true
	h := &dialer_domain.TransferHandle{
		ID:            uuid.New().String(),
		CallID:        req.CallID,
		WorkspaceID:   req.WorkspaceID,
		InitiatorID:   req.InitiatorID,
		TargetUserID:  req.TargetUserID,
		OfferedUserID: req.TargetUserID,
		Kind:          req.Kind,
		Stage:         dialer_domain.TransferStagePendingOffer,
		Note:          req.Note,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// A human blind transfer parks the caller leg at initiate (true blind: the
	// initiator is freed immediately and failures recall them). AI blind keeps the
	// AI on the line until accept (the AI bridge owns its own audio); attended
	// keeps the leg on the initiator for the consult, held from this instant.
	if human && req.Kind == dialer_domain.TransferKindBlind {
		h.Parked = true
		h.ParkDeadline = now.Add(uc.maxPark)
	}

	// One human = one call: if the target is on a call (or already being rung) on
	// ANY endpoint, they are busy — do not ring their other idle endpoints (an idle
	// browser must not mask an in-progress branch call). This mirrors the presence
	// roster and ListAvailable's offerability rule so routing stays consistent.
	for _, s := range targetSessions {
		if s != nil && s.HasActiveCall() {
			// Target busy: queue the caller instead of returning busy, when a queue
			// policy is enabled for this target (the "transfer to a busy colleague →
			// caller waits in line for them" behavior).
			if qh, queued, qErr := uc.enqueueOnBusy(ctx, req, entry); qErr != nil {
				return nil, qErr
			} else if queued {
				return qh, nil
			}
			return nil, dialer_domain.ErrTransferTargetBusy
		}
	}

	// Reserve every contact for this offer's ring (all are free per the check above).
	// Reserve is the authoritative compare-and-set per session, so a contact that goes
	// busy in the race window is still skipped. The reservation is consumed by Attach
	// on the contact that accepts and released on every other contact (loser cancel)
	// and on every non-accept outcome (Decline, CancelAttended, AbortByDisconnect,
	// the reaper Tick). Both kinds fork to every contact (browser AND branch): a SIP
	// phone can now host an attended consult too (branch consult media bridge).
	var reserved []dialer_domain.DialerSession
	for _, s := range targetSessions {
		if s == nil {
			continue
		}
		// Honor the member's ring-channel policy: never ring an endpoint whose
		// channel the member disabled (e.g. an idle browser they turned off), even
		// though it still counts toward the busy check above.
		if e, ok := s.(dialer_domain.RingEligibleSession); ok && !e.RingEligible() {
			continue
		}
		if s.Reserve(h.ID) {
			reserved = append(reserved, s)
			h.OfferedSessionIDs = append(h.OfferedSessionIDs, s.ID())
		}
	}
	if len(reserved) == 0 {
		// Every contact went busy in the race window: queue the caller if a policy
		// applies, otherwise report busy.
		if qh, queued, qErr := uc.enqueueOnBusy(ctx, req, entry); qErr != nil {
			return nil, qErr
		} else if queued {
			return qh, nil
		}
		return nil, dialer_domain.ErrTransferTargetBusy
	}

	if err := uc.store.Put(h); err != nil {
		for _, s := range reserved {
			s.Release(h.ID)
		}
		return nil, err
	}

	// Hold the caller the INSTANT the transfer starts (industry rule R1/R2: the
	// caller must never hear the initiator's mic or the consult; they hear hold
	// audio until bridged to their final party). Blind additionally parks the leg,
	// freeing the initiator. If the media arm fails, unwind fully: the offer never
	// rang anyone yet.
	if human {
		var mediaErr error
		if h.Parked {
			mediaErr = uc.executor.ParkBlind(ctx, h)
		} else {
			mediaErr = uc.executor.HoldCaller(ctx, h)
		}
		if mediaErr != nil {
			for _, s := range reserved {
				s.Release(h.ID)
			}
			uc.store.Delete(h.ID)
			uc.logger.Printf("[Transfer] initiate media setup failed for %s (parked=%v): %v", h.ID, h.Parked, mediaErr)
			return nil, mediaErr
		}
	}

	offer := dialer_domain.DialerControlMessage{
		Type: "transfer:offer",
		Payload: map[string]any{
			"transfer_id":  h.ID,
			"call_id":      h.CallID,
			"workspace_id": h.WorkspaceID,
			"initiator_id": h.InitiatorID,
			"kind":         string(h.Kind),
			"note":         h.Note,
			"phone_number": entry.Phone,
			"created_at":   h.CreatedAt,
		},
	}
	for _, s := range reserved {
		if err := s.Notify(offer); err != nil {
			// This contact never rang; free its reservation so it does not hold the
			// slot until the reaper times the handle out. Other contacts still ring.
			s.Release(h.ID)
			uc.logger.Printf("[Transfer] failed to notify target %s contact %s of offer %s: %v",
				req.TargetUserID, s.ID(), h.ID, err)
		}
	}

	// The reserved contacts are now busy-while-ringing; broadcast so the presence panel
	// reflects it in real time (Reserve/Release are otherwise invisible to presence).
	uc.sessions.NotifyPresenceChanged(h.WorkspaceID)

	return h, nil
}

// cancelOffered stops every contact of the CURRENT ring wave (except
// exceptSessionID, empty to cancel all) from ringing: it frees the reservation and
// sends eventType so a browser closes its popup and a branch cancels its INVITE
// (via its Notify handler). The wave's member is h.RingsUserID(): the transfer
// target while pending, the original initiator during a recall. This is what
// guarantees that once one channel answers OR declines, no other channel keeps
// ringing or can also answer.
// sessionOffered reports whether sessionID is one of a wave's offered contacts.
func sessionOffered(offered []string, sessionID string) bool {
	for _, sid := range offered {
		if sid == sessionID {
			return true
		}
	}
	return false
}

func (uc *CallTransferUseCase) cancelOffered(h *dialer_domain.TransferHandle, exceptSessionID, eventType, reason string) {
	if h == nil || len(h.OfferedSessionIDs) == 0 {
		return
	}
	// Resolve each offered contact by its session id directly: a ringall wave's
	// contacts span MANY members, so they cannot be found via a single RingsUserID.
	// FindByID handles single-user (rrmemory/recall) and multi-user (ringall) waves.
	for _, sid := range h.OfferedSessionIDs {
		if sid == exceptSessionID {
			continue
		}
		s, ok := uc.sessions.FindByID(sid)
		if !ok || s == nil {
			continue // the contact went away; its ring is already gone
		}
		s.Release(h.ID)
		_ = s.Notify(dialer_domain.DialerControlMessage{
			Type: eventType,
			Payload: map[string]any{
				"transfer_id": h.ID,
				"call_id":     h.CallID,
				"reason":      reason,
			},
		})
	}
	// Freed reservations mean those contacts are available again; refresh presence.
	uc.sessions.NotifyPresenceChanged(h.WorkspaceID)
}

func (uc *CallTransferUseCase) Accept(ctx context.Context, transferID, acceptingUserID, acceptingSessionID string) (*dialer_domain.TransferHandle, error) {
	var (
		captured   *dialer_domain.TransferHandle
		fromRecall bool
		fromQueue  bool
	)
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		var next dialer_domain.TransferStage
		switch h.Stage {
		case dialer_domain.TransferStagePendingOffer:
			if h.TargetUserID != acceptingUserID {
				return dialer_domain.ErrTransferNotForUser
			}
			next = dialer_domain.TransferStageCompleting
			if h.Kind == dialer_domain.TransferKindAttended && !h.EarlyCompleted {
				next = dialer_domain.TransferStageConsulting
			}
		case dialer_domain.TransferStageRecalling:
			// A recall wave rings the ORIGINAL INITIATOR; only they may accept it.
			if h.InitiatorID != acceptingUserID {
				return dialer_domain.ErrTransferNotForUser
			}
			fromRecall = true
			next = dialer_domain.TransferStageCompleting
		case dialer_domain.TransferStageQueued:
			// A queue wave rings one candidate (rrmemory) or the whole free pool at once
			// (ringall); ANY offered contact may accept, so validate on the offered
			// SESSION set rather than a single user. The stage CAS is the race gate: the
			// first contact to flip queued->completing wins; a stale/other contact can
			// never also grab the caller. Record the actual accepter so the media swap
			// and completion notify target them (ringall has no pre-chosen OfferedUserID).
			if acceptingSessionID == "" || !sessionOffered(h.OfferedSessionIDs, acceptingSessionID) {
				return dialer_domain.ErrTransferNotForUser
			}
			h.OfferedUserID = acceptingUserID
			fromQueue = true
			next = dialer_domain.TransferStageCompleting
		default:
			return dialer_domain.ErrTransferInvalidStage
		}
		// The stage compare-and-set is the race gate: only the FIRST contact to accept
		// moves the stage, so a second accept from another contact of the same member
		// fails here and can never also complete the transfer.
		if err := dialer_domain.ValidateStageTransition(h.Stage, next); err != nil {
			return err
		}
		h.Stage = next
		h.AcceptedSessionID = acceptingSessionID
		h.UpdatedAt = uc.now()
		clone := *h
		captured = &clone
		return nil
	})
	if err != nil {
		return nil, err
	}
	if captured == nil {
		return nil, dialer_domain.ErrTransferNotFound
	}

	if captured.Stage == dialer_domain.TransferStageConsulting {
		if err := uc.executor.OpenConsult(ctx, captured); err != nil {

			_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
				h.Stage = dialer_domain.TransferStageDeclined
				h.UpdatedAt = uc.now()
				return nil
			})
			// Consult never opened: the accept failed, so stop every contact ringing,
			// free their reservations, and take the caller OFF hold (they resume with
			// the initiator, who never left).
			uc.cancelOffered(captured, "", "transfer:cancelled", "transfer_failed")
			uc.executor.ReleaseHold(ctx, captured)
			return captured, err
		}

		// Consult opened on the winning contact: stop the member's OTHER contacts.
		uc.cancelOffered(captured, acceptingSessionID, "transfer:cancelled", "answered_elsewhere")
		return captured, nil
	}

	// Stage completing: the media swap decides the terminal.
	if fromQueue {
		// A queued caller's wave was accepted by candidate captured.OfferedUserID:
		// attach the parked leg to them. Success is a COMPLETED transfer (the caller
		// reached an agent). On failure the caller is safe in the park, so re-enter
		// the queue and let Tick try the next available agent.
		if err := uc.executor.SwapParked(ctx, captured, captured.OfferedUserID); err != nil {
			_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
				if h.Stage != dialer_domain.TransferStageCompleting {
					return errAbortNoop
				}
				h.Stage = dialer_domain.TransferStageQueued
				h.RecallOfferAt = time.Time{}
				h.OfferedSessionIDs = nil
				h.OfferedUserID = ""
				h.UpdatedAt = uc.now()
				return nil
			})
			// Stop this candidate's ring; the caller keeps their place in line.
			uc.cancelOffered(captured, "", "transfer:cancelled", "transfer_failed")
			return captured, err
		}
		if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageCompleted, ""); terminalErr != nil {
			uc.logger.Printf("[Transfer] queued terminalize failed for %s: %v", transferID, terminalErr)
		}
		captured.Stage = dialer_domain.TransferStageCompleted
		uc.dequeueDirector(captured, dialer_domain.QueueReasonConnected)
		uc.cancelOffered(captured, acceptingSessionID, "transfer:cancelled", "answered_elsewhere")
		uc.notifyInitiatorCompleted(captured)
		return captured, nil
	}

	if fromRecall {
		if err := uc.executor.SwapParked(ctx, captured, captured.InitiatorID); err != nil {
			// The winning contact could not take the leg back; re-enter the ladder so
			// Tick retries or falls back. Other recall contacts keep ringing.
			_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
				h.Stage = dialer_domain.TransferStageRecalling
				h.UpdatedAt = uc.now()
				return nil
			})
			return captured, err
		}
		if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageRecalled, ""); terminalErr != nil {
			uc.logger.Printf("[Transfer] recalled terminalize failed for %s: %v", transferID, terminalErr)
		}
		captured.Stage = dialer_domain.TransferStageRecalled
		uc.cancelOffered(captured, acceptingSessionID, "transfer:cancelled", "answered_elsewhere")
		return captured, nil
	}

	if err := uc.executor.SwapBlind(ctx, captured); err != nil {
		if captured.Parked {
			// The leg is safe in the park; failures roll into the recall ladder
			// instead of stranding the caller. Stop the target's ring first.
			_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
				h.Stage = dialer_domain.TransferStageRecalling
				h.UpdatedAt = uc.now()
				return nil
			})
			uc.cancelOffered(captured, "", "transfer:cancelled", "transfer_failed")
			return captured, err
		}
		_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
			h.Stage = dialer_domain.TransferStageFailed
			h.FailReason = string(dialer_domain.TransferReasonSwapFailed)
			h.UpdatedAt = uc.now()
			return nil
		})
		// Swap failed: the accept did not complete, so stop every contact ringing
		// and resume the caller with the initiator (AI keeps talking; a held
		// attended caller is un-held).
		uc.cancelOffered(captured, "", "transfer:cancelled", "transfer_failed")
		uc.executor.ReleaseHold(ctx, captured)
		return captured, err
	}

	if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageCompleted, ""); terminalErr != nil {
		uc.logger.Printf("[Transfer] completed terminalize failed for %s: %v", transferID, terminalErr)
	}
	captured.Stage = dialer_domain.TransferStageCompleted
	// The winner has the call: stop every OTHER contact of the member from ringing.
	uc.cancelOffered(captured, acceptingSessionID, "transfer:cancelled", "answered_elsewhere")
	uc.notifyInitiatorCompleted(captured)
	return captured, nil
}

// terminalize CASes a handle into a terminal stage after the media arm succeeded.
func (uc *CallTransferUseCase) terminalize(transferID string, stage dialer_domain.TransferStage, failReason string) error {
	var snap *dialer_domain.TransferHandle
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		if h.Stage.Terminal() {
			return errAbortNoop
		}
		h.Stage = stage
		if failReason != "" {
			h.FailReason = failReason
		}
		h.UpdatedAt = uc.now()
		cp := *h
		snap = &cp
		return nil
	})
	if errors.Is(err, errAbortNoop) {
		return nil
	}
	if err == nil && snap != nil {
		uc.emitTransferTelemetry(snap, stage)
	}
	return err
}

func (uc *CallTransferUseCase) emitTransferTelemetry(h *dialer_domain.TransferHandle, stage dialer_domain.TransferStage) {
	if uc == nil || uc.telemetry == nil || h == nil {
		return
	}
	uc.telemetry.OnTransferStage(h.WorkspaceID, h.CallID, h.ID, h.TargetUserID, h.Note, stage)
}

func (uc *CallTransferUseCase) Decline(ctx context.Context, transferID, targetUserID, reason string) error {
	var (
		captured *dialer_domain.TransferHandle
		toRecall bool
	)
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		switch h.Stage {
		case dialer_domain.TransferStagePendingOffer:
			if h.TargetUserID != targetUserID {
				return dialer_domain.ErrTransferNotForUser
			}
			next := dialer_domain.TransferStageDeclined
			if h.Parked {
				// The caller leg is parked: a decline is not terminal, it starts the
				// recall ladder back to the initiator (Asterisk's Recall superstate).
				next = dialer_domain.TransferStageRecalling
				toRecall = true
			}
			if err := dialer_domain.ValidateStageTransition(h.Stage, next); err != nil {
				return err
			}
			h.Stage = next
			if toRecall {
				h.RecallOfferAt = time.Time{}
			}
		case dialer_domain.TransferStageRecalling:
			// The initiator declined taking the call back: give up and run the
			// fallback (never leave the caller in infinite hold audio).
			if h.InitiatorID != targetUserID {
				return dialer_domain.ErrTransferNotForUser
			}
			if err := dialer_domain.ValidateStageTransition(h.Stage, dialer_domain.TransferStageFailed); err != nil {
				return err
			}
			h.Stage = dialer_domain.TransferStageFailed
			h.FailReason = string(dialer_domain.TransferReasonRecallDeclined)
		default:
			return dialer_domain.ErrTransferInvalidStage
		}
		h.UpdatedAt = uc.now()
		clone := *h
		captured = &clone
		return nil
	})
	if err != nil {
		return err
	}
	if captured == nil {
		return nil
	}

	// Declining on ONE device declines the whole offer (the member said no, not
	// "this device is busy"), so stop EVERY contact ringing and free their
	// reservations. Then tell the initiator what happened.
	uc.cancelOffered(captured, "", "transfer:cancelled", reason)
	switch captured.Stage {
	case dialer_domain.TransferStageDeclined:
		// The caller never left the initiator; just take them off hold.
		uc.executor.ReleaseHold(ctx, captured)
		uc.notifyInitiator(captured, "transfer:declined", reason)
	case dialer_domain.TransferStageRecalling:
		uc.notifyInitiator(captured, "transfer:declined", reason)
	case dialer_domain.TransferStageFailed:
		uc.executor.AbandonPark(ctx, captured, captured.FailReason)
		uc.notifyInitiator(captured, "transfer:cancelled", captured.FailReason)
	}
	return nil
}

func (uc *CallTransferUseCase) CompleteAttended(ctx context.Context, transferID, initiatorID string) (*dialer_domain.TransferHandle, error) {
	var (
		captured *dialer_domain.TransferHandle
		blond    bool
	)
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		if h.InitiatorID != initiatorID {
			return dialer_domain.ErrTransferNotForUser
		}
		if h.Kind != dialer_domain.TransferKindAttended {
			return dialer_domain.ErrTransferInvalidKind
		}
		switch h.Stage {
		case dialer_domain.TransferStageConsulting:
			if err := dialer_domain.ValidateStageTransition(h.Stage, dialer_domain.TransferStageCompleting); err != nil {
				return err
			}
			h.Stage = dialer_domain.TransferStageCompleting
		case dialer_domain.TransferStagePendingOffer:
			// Complete while the target is still ringing: the "blond" transfer. The
			// stage stays pending (the offer keeps ringing); the leg is parked and
			// the initiator freed, and from here the handle behaves like blind.
			if h.EarlyCompleted {
				return dialer_domain.ErrTransferInvalidStage
			}
			blond = true
			h.EarlyCompleted = true
			h.Parked = true
			h.ParkDeadline = uc.now().Add(uc.maxPark)
		default:
			return dialer_domain.ErrTransferInvalidStage
		}
		h.UpdatedAt = uc.now()
		clone := *h
		captured = &clone
		return nil
	})
	if err != nil {
		return nil, err
	}
	if captured == nil {
		return nil, dialer_domain.ErrTransferNotFound
	}

	if blond {
		if err := uc.executor.ParkBlind(ctx, captured); err != nil {
			_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
				h.EarlyCompleted = false
				h.Parked = false
				h.ParkDeadline = time.Time{}
				h.UpdatedAt = uc.now()
				return nil
			})
			return captured, err
		}
		return captured, nil
	}

	if err := uc.executor.CloseConsult(ctx, captured, true); err != nil {

		_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
			h.Stage = dialer_domain.TransferStageConsulting
			h.UpdatedAt = uc.now()
			return nil
		})
		return captured, err
	}

	if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageCompleted, ""); terminalErr != nil {
		uc.logger.Printf("[Transfer] attended terminalize failed for %s: %v", transferID, terminalErr)
	}
	captured.Stage = dialer_domain.TransferStageCompleted
	uc.notifyInitiatorCompleted(captured)
	return captured, nil
}

// CancelAttended is the initiator's cancel. Despite the historical name it now
// covers every initiator-cancellable stage: a consult in progress (attended), a
// still-ringing offer of EITHER kind (the caller is un-held or pulled back from
// the park), and an in-progress recall (which gives the caller up to the
// fallback, since the initiator is refusing the call back).
func (uc *CallTransferUseCase) CancelAttended(ctx context.Context, transferID, initiatorID, reason string) error {
	var (
		captured   *dialer_domain.TransferHandle
		wasConsult bool
		pullBack   bool
	)
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		if h.InitiatorID != initiatorID {
			return dialer_domain.ErrTransferNotForUser
		}
		switch h.Stage {
		case dialer_domain.TransferStageConsulting:
			if err := dialer_domain.ValidateStageTransition(h.Stage, dialer_domain.TransferStageCancelled); err != nil {
				return err
			}
			wasConsult = true
			h.Stage = dialer_domain.TransferStageCancelled
		case dialer_domain.TransferStagePendingOffer:
			if h.Parked {
				// Take the caller back NOW: go through completing so the retrieve is
				// guarded exactly like an accept (one winner, wedge-reaped).
				if err := dialer_domain.ValidateStageTransition(h.Stage, dialer_domain.TransferStageCompleting); err != nil {
					return err
				}
				pullBack = true
				h.Stage = dialer_domain.TransferStageCompleting
			} else {
				if err := dialer_domain.ValidateStageTransition(h.Stage, dialer_domain.TransferStageCancelled); err != nil {
					return err
				}
				h.Stage = dialer_domain.TransferStageCancelled
			}
		case dialer_domain.TransferStageRecalling:
			if err := dialer_domain.ValidateStageTransition(h.Stage, dialer_domain.TransferStageCancelled); err != nil {
				return err
			}
			h.Stage = dialer_domain.TransferStageCancelled
		default:
			return dialer_domain.ErrTransferInvalidStage
		}
		h.UpdatedAt = uc.now()
		clone := *h
		captured = &clone
		return nil
	})
	if err != nil {
		return err
	}
	if captured == nil {
		return dialer_domain.ErrTransferNotFound
	}

	// Stop the current ring wave first so the target's popup/phone dies instantly.
	uc.cancelOffered(captured, "", "transfer:cancelled", reason)

	switch {
	case wasConsult:
		if err := uc.executor.CloseConsult(ctx, captured, false); err != nil {
			uc.logger.Printf("[Transfer] CloseConsult on cancel failed for %s: %v", transferID, err)
		}
	case pullBack:
		if err := uc.executor.SwapParked(ctx, captured, captured.InitiatorID); err != nil {
			// The initiator could not take the leg back right now (raced into another
			// call): fall into the recall ladder rather than stranding the caller.
			uc.logger.Printf("[Transfer] cancel pull-back failed for %s, entering recall: %v", transferID, err)
			_, _ = uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
				h.Stage = dialer_domain.TransferStageRecalling
				h.RecallOfferAt = time.Time{}
				h.UpdatedAt = uc.now()
				return nil
			})
		} else if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageRecalled, ""); terminalErr != nil {
			uc.logger.Printf("[Transfer] cancel terminalize failed for %s: %v", transferID, terminalErr)
		}
	case captured.Parked:
		// Cancelled while recalling: the initiator refuses the call back; run the
		// fallback so the parked caller is never stranded.
		uc.executor.AbandonPark(ctx, captured, string(dialer_domain.TransferReasonInitiatorCancelled))
	default:
		// A held (attended, pre-accept) caller resumes with the initiator.
		uc.executor.ReleaseHold(ctx, captured)
	}

	uc.notifyInitiator(captured, "transfer:cancelled", reason)
	return nil
}

func (uc *CallTransferUseCase) notifyInitiatorCompleted(h *dialer_domain.TransferHandle) {
	uc.notifyInitiator(h, "transfer:completed", "")
}

func (uc *CallTransferUseCase) notifyInitiator(h *dialer_domain.TransferHandle, eventType, reason string) {
	initiator, ok := uc.sessions.FindByUser(h.WorkspaceID, h.InitiatorID)
	if !ok {
		return
	}
	payload := map[string]any{
		"transfer_id":    h.ID,
		"call_id":        h.CallID,
		"target_user_id": h.TargetUserID,
		"stage":          string(h.Stage),
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if err := initiator.Notify(dialer_domain.DialerControlMessage{Type: eventType, Payload: payload}); err != nil {
		uc.logger.Printf("[Transfer] failed to notify initiator %s (%s): %v",
			h.InitiatorID, eventType, err)
	}
}

// AbortByDisconnect handles a consult participant's socket dropping. The reason
// carries the ROLE (initiator_disconnect / target_disconnect, wired by the
// executor's per-role hooks; the session's generic participant_disconnect is
// treated as unknown). Asterisk semantics for the initiator dropping mid-consult:
// COMPLETE the transfer — the target is already engaged and context-loaded;
// killing the customer's call (the old behavior) is the worst outcome.
func (uc *CallTransferUseCase) AbortByDisconnect(ctx context.Context, transferID, reason string) error {
	var (
		captured       *dialer_domain.TransferHandle
		wasConsult     bool
		completeOnDrop bool
	)
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		if h.Stage.Terminal() {
			return errAbortNoop
		}
		wasConsult = h.Stage == dialer_domain.TransferStageConsulting
		if wasConsult && reason == string(dialer_domain.TransferReasonInitiatorDisconnect) {
			completeOnDrop = true
			h.Stage = dialer_domain.TransferStageCompleting
		} else {
			h.Stage = dialer_domain.TransferStageCancelled
		}
		h.UpdatedAt = uc.now()
		clone := *h
		captured = &clone
		return nil
	})
	if errors.Is(err, errAbortNoop) {
		return nil
	}
	if err != nil {
		return err
	}
	if captured == nil {
		return nil
	}

	if completeOnDrop {
		if cerr := uc.executor.CloseConsult(ctx, captured, true); cerr == nil {
			if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageCompleted, ""); terminalErr != nil {
				uc.logger.Printf("[Transfer] disconnect-complete terminalize failed for %s: %v", transferID, terminalErr)
			}
			uc.cancelOffered(captured, captured.AcceptedSessionID, "transfer:cancelled", "answered_elsewhere")
			uc.notifyInitiator(captured, "transfer:completed", reason)
			return nil
		} else {
			// Could not promote (target also gone / attach failed): fall through to
			// a plain cancel so the session teardown can reclaim the leg.
			uc.logger.Printf("[Transfer] complete-on-disconnect failed for %s, cancelling: %v", transferID, cerr)
			if terminalErr := uc.terminalize(transferID, dialer_domain.TransferStageFailed, string(dialer_domain.TransferReasonSwapFailed)); terminalErr != nil {
				uc.logger.Printf("[Transfer] disconnect-fail terminalize failed for %s: %v", transferID, terminalErr)
			}
			if cerr := uc.executor.CloseConsult(ctx, captured, false); cerr != nil {
				uc.logger.Printf("[Transfer] CloseConsult on failed promote for %s: %v", transferID, cerr)
			}
			uc.cancelOffered(captured, "", "transfer:cancelled", reason)
			uc.notifyInitiator(captured, "transfer:cancelled", reason)
			return nil
		}
	}

	if wasConsult {
		if cerr := uc.executor.CloseConsult(ctx, captured, false); cerr != nil {
			uc.logger.Printf("[Transfer] CloseConsult on disconnect-abort failed for %s: %v", transferID, cerr)
		}
	} else if !captured.Parked {
		// A pending held caller resumes with the initiator (if the INITIATOR is the
		// one who dropped, their session teardown ends the call right after; a
		// parked leg is untouched — the offer keeps ringing without them).
		uc.executor.ReleaseHold(ctx, captured)
	}

	// Disconnect aborted the transfer: stop every contact ringing and free their
	// reservations (covers a pending offer or an in-progress attended consult).
	uc.cancelOffered(captured, "", "transfer:cancelled", reason)
	if captured.Parked {
		uc.executor.AbandonPark(ctx, captured, reason)
	}
	uc.notifyInitiator(captured, "transfer:cancelled", reason)
	return nil
}

// AbortByCall aborts the pending offer for a call (if any). It is the safety net for
// the initiator hanging up or disconnecting mid-transfer: without it the offer stays
// pending, every contact keeps ringing, and their reservations leak (which also makes
// the target look busy and drop out of the transfer picker).
func (uc *CallTransferUseCase) AbortByCall(ctx context.Context, workspaceID, callID, reason string) error {
	h, ok := uc.store.FindByCall(workspaceID, callID)
	if !ok || h == nil {
		return nil
	}
	return uc.AbortByDisconnect(ctx, h.ID, reason)
}

// AbortByCallLegDeath is the customer-hangup funnel (see the domain interface).
func (uc *CallTransferUseCase) AbortByCallLegDeath(ctx context.Context, workspaceID, callID string) error {
	h, ok := uc.store.FindByCall(workspaceID, callID)
	if !ok || h == nil {
		return nil
	}
	var (
		captured   *dialer_domain.TransferHandle
		wasConsult bool
	)
	_, err := uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage.Terminal() {
			return errAbortNoop
		}
		wasConsult = t.Stage == dialer_domain.TransferStageConsulting
		t.Stage = dialer_domain.TransferStageFailed
		t.FailReason = string(dialer_domain.TransferReasonCustomerHangup)
		t.UpdatedAt = uc.now()
		clone := *t
		captured = &clone
		return nil
	})
	if errors.Is(err, errAbortNoop) {
		return nil
	}
	if err != nil {
		return err
	}
	if captured == nil {
		return nil
	}

	uc.logger.Printf("[Transfer] caller hung up mid-transfer %s (call %s): aborting", captured.ID, callID)

	if wasConsult {
		// Tears the consult down and notifies BOTH agents; the leg is already dead
		// so the un-hold inside is a no-op.
		if cerr := uc.executor.CloseConsult(ctx, captured, false); cerr != nil {
			uc.logger.Printf("[Transfer] CloseConsult on customer hangup failed for %s: %v", captured.ID, cerr)
		}
	}
	// Stop every ringing contact INSTANTLY (this is what kills the 30s ghost ring).
	uc.cancelOffered(captured, "", "transfer:cancelled", string(dialer_domain.TransferReasonCustomerHangup))
	if captured.Parked {
		// The park death watcher already removed the entry; this is a defensive
		// cleanup for non-watcher paths and is a no-op otherwise.
		uc.executor.AbandonPark(ctx, captured, string(dialer_domain.TransferReasonCustomerHangup))
	}
	// A caller who hangs up while queued must leave the line (exactly-once; Remove is
	// idempotent so the park watcher and this funnel can't double-remove).
	uc.dequeueDirector(captured, dialer_domain.QueueReasonAbandoned)
	if !wasConsult {
		uc.notifyInitiator(captured, "transfer:cancelled", string(dialer_domain.TransferReasonCustomerHangup))
	}
	return nil
}

// FindActiveByCall exposes the in-flight handle for stage-aware delivery decisions.
func (uc *CallTransferUseCase) FindActiveByCall(workspaceID, callID string) (*dialer_domain.TransferHandle, bool) {
	h, ok := uc.store.FindByCall(workspaceID, callID)
	if !ok || h == nil || h.Stage.Terminal() {
		return nil, false
	}
	return h, true
}

var errAbortNoop = errors.New("transfer abort: already terminal")

// ReapExpiredOffers expires stale pending offers relative to cutoff. Kept for
// interface compatibility; Tick is the full time driver and calls this logic with
// the configured TTL. Parked (blind/blond) handles recall instead of timing out.
func (uc *CallTransferUseCase) ReapExpiredOffers(ctx context.Context, cutoff time.Time) int {
	n := 0
	for _, h := range uc.store.ListByStage(dialer_domain.TransferStagePendingOffer) {
		if h == nil || h.UpdatedAt.After(cutoff) {
			continue
		}
		if uc.expirePending(ctx, h.ID) {
			n++
		}
	}
	return n
}

// expirePending times a pending offer out through the same CAS as every other
// actor. Returns whether this call performed the transition.
func (uc *CallTransferUseCase) expirePending(ctx context.Context, transferID string) bool {
	var captured *dialer_domain.TransferHandle
	_, err := uc.store.Update(transferID, func(h *dialer_domain.TransferHandle) error {
		if h.Stage != dialer_domain.TransferStagePendingOffer {
			return errAbortNoop
		}
		if h.Parked {
			h.Stage = dialer_domain.TransferStageRecalling
			h.RecallOfferAt = time.Time{}
		} else {
			h.Stage = dialer_domain.TransferStageTimedOut
		}
		h.UpdatedAt = uc.now()
		clone := *h
		captured = &clone
		return nil
	})
	if err != nil || captured == nil {
		return false
	}

	// Offer timed out in the ring window: stop every contact ringing and release
	// their reservations so a never-answered transfer doesn't keep the member
	// marked busy.
	uc.cancelOffered(captured, "", "transfer:timed_out", "offer_expired")
	if captured.Stage == dialer_domain.TransferStageTimedOut {
		uc.executor.ReleaseHold(ctx, captured)
	}
	uc.notifyInitiator(captured, "transfer:timed_out", "offer_expired")
	return true
}

// Tick drives every time-based transition (called by the container reaper loop).
func (uc *CallTransferUseCase) Tick(ctx context.Context, now time.Time) int {
	n := uc.ReapExpiredOffers(ctx, now.Add(-uc.offerTTL))

	// Recall ladder: expire stale waves, start new ones paced by recallDelay,
	// enforce the park deadline, fall back when retries are exhausted.
	for _, h := range uc.store.ListByStage(dialer_domain.TransferStageRecalling) {
		if h == nil {
			continue
		}
		switch {
		case !h.ParkDeadline.IsZero() && now.After(h.ParkDeadline):
			if uc.failRecall(ctx, h.ID, string(dialer_domain.TransferReasonParkDeadline)) {
				n++
			}
		case !h.RecallOfferAt.IsZero() && now.After(h.RecallOfferAt.Add(uc.recallOfferWindow)):
			// The outstanding recall wave rang out unanswered: close it (release the
			// initiator's reservations); the next tick starts the next wave or the
			// fallback, giving the ladder its loop delay naturally.
			uc.closeRecallWave(h)
			n++
		case h.RecallOfferAt.IsZero():
			if now.Sub(h.UpdatedAt) < uc.recallDelay {
				continue // pace between waves (atxferloopdelay)
			}
			if h.RecallAttempts >= uc.recallRetries {
				if uc.failRecall(ctx, h.ID, string(dialer_domain.TransferReasonRecallExhausted)) {
					n++
				}
				continue
			}
			if uc.startRecallWave(h) {
				n++
			}
		}
	}

	// Wedged completing handles: an executor that never came back may no longer be
	// trusted to finish; fail the handle so reservations and the park are reclaimed.
	for _, h := range uc.store.ListByStage(dialer_domain.TransferStageCompleting) {
		if h == nil || now.Sub(h.UpdatedAt) < defaultCompletingWedgeExpiry {
			continue
		}
		var captured *dialer_domain.TransferHandle
		_, err := uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
			if t.Stage != dialer_domain.TransferStageCompleting {
				return errAbortNoop
			}
			t.Stage = dialer_domain.TransferStageFailed
			t.FailReason = string(dialer_domain.TransferReasonExecutorWedged)
			t.UpdatedAt = uc.now()
			clone := *t
			captured = &clone
			return nil
		})
		if err != nil || captured == nil {
			continue
		}
		uc.logger.Printf("[Transfer] completing handle %s wedged >%s: failing", h.ID, defaultCompletingWedgeExpiry)
		uc.cancelOffered(captured, "", "transfer:cancelled", captured.FailReason)
		if captured.Parked {
			uc.executor.AbandonPark(ctx, captured, captured.FailReason)
		} else {
			uc.executor.ReleaseHold(ctx, captured)
		}
		uc.notifyInitiator(captured, "transfer:cancelled", captured.FailReason)
		n++
	}

	// ACD waiting line: ring the target pool in waves, enforce MaxWait / overflow.
	n += uc.tickQueued(ctx, now)
	return n
}

// closeRecallWave releases an unanswered recall wave's reservations and marks the
// wave closed (RecallOfferAt zero) so the next tick paces the next attempt.
func (uc *CallTransferUseCase) closeRecallWave(h *dialer_domain.TransferHandle) {
	var captured *dialer_domain.TransferHandle
	_, err := uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageRecalling || t.RecallOfferAt.IsZero() {
			return errAbortNoop
		}
		clone := *t
		captured = &clone // capture the wave's session ids BEFORE clearing
		t.RecallOfferAt = time.Time{}
		t.OfferedSessionIDs = nil
		t.UpdatedAt = uc.now()
		return nil
	})
	if err != nil || captured == nil {
		return
	}
	uc.cancelOffered(captured, "", "transfer:timed_out", "recall_no_answer")
}

// startRecallWave reserves and rings the ORIGINAL INITIATOR's contacts so they can
// take the parked caller back. An attempt is consumed even when nobody was
// reservable (initiator busy/offline), exactly like Asterisk counting a failed
// callback toward atxfercallbackretries.
func (uc *CallTransferUseCase) startRecallWave(h *dialer_domain.TransferHandle) bool {
	entryPhone := ""
	if entry, ok := uc.calls.Lookup(h.WorkspaceID, h.CallID); ok {
		entryPhone = entry.Phone
	}

	var reserved []dialer_domain.DialerSession
	for _, s := range uc.sessions.FindSessionsByUser(h.WorkspaceID, h.InitiatorID) {
		if s == nil {
			continue
		}
		if e, ok := s.(dialer_domain.RingEligibleSession); ok && !e.RingEligible() {
			continue
		}
		if s.Reserve(h.ID) {
			reserved = append(reserved, s)
		}
	}

	ids := make([]string, 0, len(reserved))
	for _, s := range reserved {
		ids = append(ids, s.ID())
	}

	var captured *dialer_domain.TransferHandle
	_, err := uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageRecalling || !t.RecallOfferAt.IsZero() {
			return errAbortNoop
		}
		t.RecallAttempts++
		t.RecallOfferAt = uc.now()
		t.OfferedSessionIDs = ids
		t.OfferedUserID = t.InitiatorID
		t.UpdatedAt = uc.now()
		clone := *t
		captured = &clone
		return nil
	})
	if err != nil || captured == nil {
		for _, s := range reserved {
			s.Release(h.ID)
		}
		return false
	}

	offer := dialer_domain.DialerControlMessage{
		Type: "transfer:offer",
		Payload: map[string]any{
			"transfer_id":  captured.ID,
			"call_id":      captured.CallID,
			"workspace_id": captured.WorkspaceID,
			"initiator_id": captured.InitiatorID,
			"kind":         string(captured.Kind),
			"note":         captured.Note,
			"phone_number": entryPhone,
			"recall":       true,
			"created_at":   captured.CreatedAt,
		},
	}
	for _, s := range reserved {
		if err := s.Notify(offer); err != nil {
			s.Release(captured.ID)
			uc.logger.Printf("[Transfer] recall notify failed for %s contact %s: %v", captured.ID, s.ID(), err)
		}
	}
	uc.sessions.NotifyPresenceChanged(captured.WorkspaceID)
	uc.logger.Printf("[Transfer] recall wave %d/%d for %s rings %d contact(s) of initiator %s",
		captured.RecallAttempts, uc.recallRetries, captured.ID, len(reserved), captured.InitiatorID)
	return true
}

// failRecall terminalizes a recall that can no longer succeed and runs the park
// fallback so the caller is never stranded in hold audio.
func (uc *CallTransferUseCase) failRecall(ctx context.Context, transferID, reason string) bool {
	var captured *dialer_domain.TransferHandle
	_, err := uc.store.Update(transferID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageRecalling {
			return errAbortNoop
		}
		t.Stage = dialer_domain.TransferStageFailed
		t.FailReason = reason
		t.UpdatedAt = uc.now()
		clone := *t
		captured = &clone
		return nil
	})
	if err != nil || captured == nil {
		return false
	}
	uc.logger.Printf("[Transfer] recall for %s failed (%s): running park fallback", transferID, reason)
	uc.cancelOffered(captured, "", "transfer:cancelled", reason)
	uc.executor.AbandonPark(ctx, captured, reason)
	uc.notifyInitiator(captured, "transfer:cancelled", reason)
	return true
}

// --- Queue (ACD waiting line) -----------------------------------------------
//
// A queued caller is a parked leg (identical to a recall) whose ring target is the
// director's available pool for the caller's QueueTarget instead of the original
// initiator. The queued stage is driven entirely by Tick, reusing the SAME park /
// reservation / SwapParked / cancelOffered machinery as the recall ladder — the
// only queue-specific logic is candidate selection (delegated to the director) and
// the MaxWait/overflow bounds. All state transitions go through the store's Update
// CAS, exactly like every other actor, so there is no queue-specific race surface.

// dequeueDirector removes a handle from its waiting line (idempotent; no-op when
// queuing is disabled or the handle was never queued).
func (uc *CallTransferUseCase) dequeueDirector(h *dialer_domain.TransferHandle, reason string) {
	if uc.queue == nil || h == nil || h.QueueKind == "" {
		return
	}
	uc.queue.Remove(context.Background(), h.WorkspaceID, h.ID, reason)
}

// Enqueue is the public entrypoint that parks a caller and places them in the ACD
// queue for a target. It is used by every automated surface that could not connect
// an agent immediately (the roulette on exhaustion, the AI transfer tools on busy,
// inbound routing with no free agent). Returns ErrQueueDisabled when no policy is
// enabled for the target (the caller should fall back to its non-queue behavior)
// and ErrQueueFull when the line is at capacity (the just-parked leg is abandoned).
func (uc *CallTransferUseCase) Enqueue(ctx context.Context, req dialer_domain.EnqueueRequest) (*dialer_domain.TransferHandle, int, error) {
	if uc.queue == nil {
		return nil, 0, dialer_domain.ErrQueueDisabled
	}
	if err := req.Validate(); err != nil {
		return nil, 0, err
	}
	policy := uc.queue.Policy(ctx, req.WorkspaceID, req.Target)
	if !policy.Enabled {
		return nil, 0, dialer_domain.ErrQueueDisabled
	}
	h, pos, err := uc.enterQueue(ctx, req.WorkspaceID, req.CallID, req.InitiatorID, req.Note, req.Target, policy)
	if err != nil {
		return nil, 0, err
	}
	uc.logger.Printf("[Transfer] enqueue call=%s target=%s/%s -> queued as %s at position %d",
		req.CallID, req.Target.Kind, req.Target.ID, h.ID, pos)
	// Best-effort: a human initiator's UI is told; an AI owner has no session.
	uc.notifyInitiator(h, "transfer:queued", "")
	return h, pos, nil
}

// enterQueue creates a queued handle for an already-owned call, parks its caller leg
// (AI-surrendered for an AI owner, detached-from-browser for a human) and admits it
// to the director's ordered line. It is the single shared body behind Enqueue and
// the human camp-on (enqueueOnBusy). Returns ErrQueueFull (after abandoning the
// just-parked leg) when the line is at MaxLength, so no caller is ever parked
// without a bound.
func (uc *CallTransferUseCase) enterQueue(ctx context.Context, workspaceID, callID, initiatorID, note string, target dialer_domain.QueueTarget, policy dialer_domain.QueuePolicy) (*dialer_domain.TransferHandle, int, error) {
	now := uc.now()
	h := &dialer_domain.TransferHandle{
		ID:          uuid.New().String(),
		CallID:      callID,
		WorkspaceID: workspaceID,
		InitiatorID: initiatorID,
		Kind:        dialer_domain.TransferKindBlind,
		Stage:       dialer_domain.TransferStageQueued,
		Note:        note,
		Parked:      true,
		QueueKind:   target.Kind,
		QueueID:     target.ID,
		EnqueuedAt:  now,
		// The hard "never wait forever" ceiling: MaxWait via the same ParkDeadline
		// the recall ladder already enforces in Tick.
		ParkDeadline: now.Add(policy.MaxWait),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	// An agent-target camp-on records TargetUserID too, so a decline/accept keys on it.
	if target.Kind == dialer_domain.QueueTargetAgent {
		h.TargetUserID = target.ID
	}
	if err := uc.store.Put(h); err != nil {
		return nil, 0, err
	}

	// Park the caller leg: the initiator's leg is detached from their browser and
	// left parked (MOH) under the death watcher. Unwind the store entry on failure.
	parkErr := uc.executor.ParkBlind(ctx, h)
	if parkErr != nil {
		uc.store.Delete(h.ID)
		uc.logger.Printf("[Transfer] enqueue park failed for %s: %v", h.ID, parkErr)
		return nil, 0, parkErr
	}

	entryPhone := ""
	if entry, ok := uc.calls.Lookup(workspaceID, callID); ok {
		entryPhone = entry.Phone
	}
	pos, aerr := uc.queue.Admit(ctx, h.AsQueuedCaller(entryPhone), policy)
	if aerr != nil {
		// Full line: never park a caller who can't be bounded — abandon and report.
		_, _ = uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
			if t.Stage.Terminal() {
				return errAbortNoop
			}
			t.Stage = dialer_domain.TransferStageFailed
			t.FailReason = string(dialer_domain.TransferReasonQueueOverflow)
			t.UpdatedAt = uc.now()
			return nil
		})
		uc.executor.AbandonPark(ctx, h, dialer_domain.QueueReasonFull)
		uc.logger.Printf("[Transfer] enqueue %s rejected (%v): line full, abandoning", h.ID, aerr)
		return nil, 0, aerr
	}

	uc.sessions.NotifyPresenceChanged(workspaceID)

	// Ring the first wave IMMEDIATELY rather than waiting for the reaper's next tick +
	// pace: a free agent must ring the instant the caller is enqueued (this is what the
	// old roulette did synchronously). rrmemory rings one candidate, ringall rings the
	// whole free pool. Subsequent waves (rrmemory only) are reaper-driven.
	uc.startQueueWave(ctx, h, !policy.Holds())
	return h, pos, nil
}

// enqueueOnBusy parks the caller and enters the queue when a HUMAN blind transfer's
// target is busy/offline and a queue policy is enabled for it. Returns
// (handle, true, nil) when the caller was queued, (nil, false, nil) when queuing
// does not apply (the caller then gets the normal busy error). Only human blind
// transfers can be queued this way: the leg is detached from the initiator and
// parked, which is exactly the true-blind media setup.
func (uc *CallTransferUseCase) enqueueOnBusy(ctx context.Context, req dialer_domain.TransferRequest, entry dialer_domain.DialerCallEntry) (*dialer_domain.TransferHandle, bool, error) {
	_ = entry
	if uc.queue == nil {
		return nil, false, nil
	}
	if req.Kind != dialer_domain.TransferKindBlind {
		return nil, false, nil
	}
	target := dialer_domain.QueueTarget{Kind: dialer_domain.QueueTargetAgent, ID: req.TargetUserID}
	policy := uc.queue.Policy(ctx, req.WorkspaceID, target)
	if !policy.Enabled {
		return nil, false, nil
	}

	h, pos, err := uc.enterQueue(ctx, req.WorkspaceID, req.CallID, req.InitiatorID, req.Note, target, policy)
	if errors.Is(err, dialer_domain.ErrQueueFull) {
		// A full line reads as busy to the initiator (the leg was already abandoned).
		return nil, false, dialer_domain.ErrTransferTargetBusy
	}
	if err != nil {
		return nil, false, err
	}

	uc.logger.Printf("[Transfer] blind transfer to busy %s: caller queued as %s at position %d",
		req.TargetUserID, h.ID, pos)
	uc.notifyInitiator(h, "transfer:queued", "")
	return h, true, nil
}

// tickQueued drives every queued handle: enforce MaxWait (overflow), close a rung-
// out wave, and pace the next wave. Mirrors the recall ladder branch of Tick.
func (uc *CallTransferUseCase) tickQueued(ctx context.Context, now time.Time) int {
	if uc.queue == nil {
		return 0
	}
	n := 0
	for _, h := range uc.store.ListByStage(dialer_domain.TransferStageQueued) {
		if h == nil {
			continue
		}
		// ringall (toggle off, no hold) does exactly ONE simultaneous pass: it never
		// closes-and-re-rings, and once its wave times out (or it found nobody) the
		// caller overflows immediately instead of holding. rrmemory (toggle on, ACD)
		// rings one at a time and holds up to MaxWait.
		ringAll := !uc.queue.Policy(ctx, h.WorkspaceID, h.QueueTarget()).Holds()
		switch {
		case !h.ParkDeadline.IsZero() && now.After(h.ParkDeadline):
			if uc.overflowQueue(ctx, h.ID) {
				n++
			}
		case ringAll && h.RecallOfferAt.IsZero():
			// The immediate ringall wave found nobody free (or already ran): no holding,
			// overflow now.
			if uc.overflowQueue(ctx, h.ID) {
				n++
			}
		case !h.RecallOfferAt.IsZero() && now.After(h.RecallOfferAt.Add(uc.recallOfferWindow)):
			if ringAll {
				// The single ringall pass rang everyone and nobody answered: overflow.
				if uc.overflowQueue(ctx, h.ID) {
					n++
				}
			} else {
				// rrmemory: the rung candidate did not answer; close the wave (free its
				// reservation). The next tick rings the next available agent.
				uc.closeQueueWave(h)
				n++
			}
		case h.RecallOfferAt.IsZero():
			if now.Sub(h.UpdatedAt) < uc.recallDelay {
				continue // pace between waves
			}
			if uc.startQueueWave(ctx, h, false) {
				n++
			}
		}
	}
	return n
}

// startQueueWave rings a queued caller's candidates. With ringAll=false (rrmemory) it
// reserves the FIRST candidate whose contacts it can grab (sequential); with
// ringAll=true it reserves EVERY available candidate for one simultaneous wave. The
// reservation CAS partitions agents across concurrently-queued callers so no agent is
// ever double-rung. Returns whether a wave was started.
func (uc *CallTransferUseCase) startQueueWave(ctx context.Context, h *dialer_domain.TransferHandle, ringAll bool) bool {
	entryPhone := ""
	if entry, ok := uc.calls.Lookup(h.WorkspaceID, h.CallID); ok {
		entryPhone = entry.Phone
	}
	candidates, err := uc.queue.Candidates(ctx, h.AsQueuedCaller(entryPhone))
	if err != nil {
		uc.logger.Printf("[Transfer] queue %s candidate resolution failed: %v", h.ID, err)
		return false
	}

	var (
		chosen   string
		reserved []dialer_domain.DialerSession
	)
	for _, uid := range candidates {
		if uid == "" || uid == h.InitiatorID {
			continue
		}
		var got []dialer_domain.DialerSession
		for _, s := range uc.sessions.FindSessionsByUser(h.WorkspaceID, uid) {
			if s == nil {
				continue
			}
			if e, ok := s.(dialer_domain.RingEligibleSession); ok && !e.RingEligible() {
				continue
			}
			if s.Reserve(h.ID) {
				got = append(got, s)
			}
		}
		if len(got) > 0 {
			reserved = append(reserved, got...)
			if chosen == "" {
				chosen = uid
			}
			if !ringAll {
				break // rrmemory: one candidate per wave
			}
		}
	}
	if len(reserved) == 0 {
		return false // nobody free right now; the next tick tries again
	}
	// ringall rings multiple members at once, so there is no single OfferedUserID; the
	// accepter is recorded when they answer. rrmemory rings one, so it keeps that user.
	offeredUser := chosen
	if ringAll {
		offeredUser = ""
	}

	ids := make([]string, 0, len(reserved))
	for _, s := range reserved {
		ids = append(ids, s.ID())
	}

	var captured *dialer_domain.TransferHandle
	_, err = uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageQueued || !t.RecallOfferAt.IsZero() {
			return errAbortNoop
		}
		t.RecallOfferAt = uc.now()
		t.OfferedSessionIDs = ids
		t.OfferedUserID = offeredUser
		t.UpdatedAt = uc.now()
		clone := *t
		captured = &clone
		return nil
	})
	if err != nil || captured == nil {
		for _, s := range reserved {
			s.Release(h.ID)
		}
		return false
	}

	offer := dialer_domain.DialerControlMessage{
		Type: "transfer:offer",
		Payload: map[string]any{
			"transfer_id":  captured.ID,
			"call_id":      captured.CallID,
			"workspace_id": captured.WorkspaceID,
			"initiator_id": captured.InitiatorID,
			"kind":         string(captured.Kind),
			"note":         captured.Note,
			"phone_number": entryPhone,
			"queued":       true,
			"created_at":   captured.CreatedAt,
		},
	}
	for _, s := range reserved {
		if err := s.Notify(offer); err != nil {
			s.Release(captured.ID)
			uc.logger.Printf("[Transfer] queue offer notify failed for %s contact %s: %v", captured.ID, s.ID(), err)
		}
	}
	uc.sessions.NotifyPresenceChanged(captured.WorkspaceID)
	return true
}

// closeQueueWave releases an unanswered queue wave's reservation and clears the
// wave so the next tick rings the next candidate. Captures the wave BEFORE clearing
// so the ring is actually cancelled.
func (uc *CallTransferUseCase) closeQueueWave(h *dialer_domain.TransferHandle) {
	var wave *dialer_domain.TransferHandle
	_, err := uc.store.Update(h.ID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageQueued || t.RecallOfferAt.IsZero() {
			return errAbortNoop
		}
		clone := *t
		wave = &clone
		t.RecallOfferAt = time.Time{}
		t.OfferedSessionIDs = nil
		t.OfferedUserID = ""
		t.UpdatedAt = uc.now()
		return nil
	})
	if err != nil || wave == nil {
		return
	}
	uc.cancelOffered(wave, "", "transfer:timed_out", "queue_no_answer")
}

// overflowQueue terminalizes a queued caller who reached MaxWait. The action is
// resolved from the policy: recall the original initiator (only for a human-
// initiated transfer) or the safe default of announce + hangup.
func (uc *CallTransferUseCase) overflowQueue(ctx context.Context, transferID string) bool {
	h, ok := uc.store.Get(transferID)
	if !ok || h == nil {
		return false
	}
	action := dialer_domain.QueueOverflowHangup
	if uc.queue != nil {
		action = uc.queue.Policy(ctx, h.WorkspaceID, h.QueueTarget()).Overflow
	}
	humanInitiator := h.InitiatorID != ""
	if action == dialer_domain.QueueOverflowRecall && humanInitiator {
		return uc.overflowToRecall(transferID)
	}

	var captured *dialer_domain.TransferHandle
	_, err := uc.store.Update(transferID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageQueued {
			return errAbortNoop
		}
		t.Stage = dialer_domain.TransferStageFailed
		t.FailReason = string(dialer_domain.TransferReasonQueueOverflow)
		t.UpdatedAt = uc.now()
		clone := *t
		captured = &clone
		return nil
	})
	if err != nil || captured == nil {
		return false
	}
	uc.logger.Printf("[Transfer] queued caller %s hit MaxWait: running overflow (hangup)", transferID)
	uc.cancelOffered(captured, "", "transfer:cancelled", captured.FailReason)
	uc.executor.AbandonPark(ctx, captured, captured.FailReason)
	uc.dequeueDirector(captured, dialer_domain.QueueReasonOverflow)
	uc.notifyInitiator(captured, "transfer:cancelled", captured.FailReason)
	return true
}

// overflowToRecall hands a timed-out queued caller to the recall ladder (ring the
// original initiator). Removes them from the line and extends the park ceiling so
// the recall ladder gets its own window.
func (uc *CallTransferUseCase) overflowToRecall(transferID string) bool {
	var wave *dialer_domain.TransferHandle
	_, err := uc.store.Update(transferID, func(t *dialer_domain.TransferHandle) error {
		if t.Stage != dialer_domain.TransferStageQueued {
			return errAbortNoop
		}
		clone := *t
		wave = &clone // capture the current wave BEFORE clearing so its ring is cancelled
		t.Stage = dialer_domain.TransferStageRecalling
		t.RecallAttempts = 0
		t.RecallOfferAt = time.Time{}
		t.OfferedSessionIDs = nil
		t.OfferedUserID = ""
		t.ParkDeadline = uc.now().Add(uc.maxPark)
		t.UpdatedAt = uc.now()
		return nil
	})
	if err != nil || wave == nil {
		return false
	}
	uc.logger.Printf("[Transfer] queued caller %s hit MaxWait: falling back to recall", transferID)
	uc.cancelOffered(wave, "", "transfer:timed_out", "queue_overflow_recall")
	uc.dequeueDirector(wave, dialer_domain.QueueReasonOverflow)
	return true
}
