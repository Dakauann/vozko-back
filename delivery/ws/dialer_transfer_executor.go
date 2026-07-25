package ws

import (
	"context"
	"errors"
	"log"
	"time"

	dialer_domain "vozko/domain/dialer"
	dialer_infra "vozko/infra/dialer"
)

type transferAborter interface {
	AbortByDisconnect(ctx context.Context, transferID, reason string) error
	AbortByCallLegDeath(ctx context.Context, workspaceID, callID string) error
}

// consultCapable is the endpoint-side port for the attended consult: attach one
// end of the private PCM bridge and carry the disconnect context. The browser
// dialerSession implements it; a branch session can implement it by pumping the
// consult PCM over its RTP leg (plan §4.8) — the executor no longer cares which.
type consultCapable interface {
	AttachConsult(endpoint *dialer_infra.ConsultEndpoint) error
	DetachConsult()
	SetTransferContext(transferID string, aborter func(transferID, reason string))
	ClearTransferContext()
}

// consultAborter is an OPTIONAL target capability: tear down the target's own
// media when an attended consult is CANCELLED (not completed). A branch implements
// it to BYE its answered phone; a browser target has nothing to tear down and does
// not implement it, so the cancel path is a no-op for browsers.
type consultAborter interface {
	AbortConsult()
}

type dialerTransferExecutor struct {
	sessions    dialer_domain.DialerSessionRegistry
	calls       dialer_domain.DialerCallRegistry
	aborter     transferAborter
	park        dialer_domain.ParkRegistry
	holdAudio   dialer_infra.HoldAudioFactory
	fallbackPCM []byte
	logger      *log.Logger
}

func NewDialerTransferExecutor(
	sessions dialer_domain.DialerSessionRegistry,
	calls dialer_domain.DialerCallRegistry,
) *dialerTransferExecutor {
	return &dialerTransferExecutor{sessions: sessions, calls: calls, logger: log.Default()}
}

func (x *dialerTransferExecutor) SetAborter(a transferAborter) { x.aborter = a }

// SetParkRegistry wires the parked-leg store (true blind / blond / recall).
func (x *dialerTransferExecutor) SetParkRegistry(p dialer_domain.ParkRegistry) { x.park = p }

// SetHoldAudio wires the per-leg hold audio factory (music on hold / comfort
// tone). Nil falls back to the generated comfort tone.
func (x *dialerTransferExecutor) SetHoldAudio(f dialer_infra.HoldAudioFactory) { x.holdAudio = f }

// SetFallbackAnnouncement wires the PCM played to a parked caller nobody could
// take before hanging them up (16-bit LE mono 8kHz). Nil hangs up directly.
func (x *dialerTransferExecutor) SetFallbackAnnouncement(pcm []byte) { x.fallbackPCM = pcm }

func (x *dialerTransferExecutor) SetLogger(l *log.Logger) {
	if l != nil {
		x.logger = l
	}
}

// roleHook builds the consult disconnect hook for one participant, binding WHO
// dropped so the use case can apply role-aware semantics (initiator drop mid
// consult completes the transfer; target drop cancels it). The reason the
// session passes at shutdown is ignored in favor of the bound role.
func (x *dialerTransferExecutor) roleHook(role string) func(transferID, reason string) {
	if x.aborter == nil {
		return nil
	}
	aborter := x.aborter
	return func(transferID, _ string) {
		_ = aborter.AbortByDisconnect(context.Background(), transferID, role)
	}
}

// legDeathHook builds the parked-leg death callback: the caller hung up while
// waiting in the park, so the whole in-flight transfer is aborted instantly.
func (x *dialerTransferExecutor) legDeathHook(workspaceID, callID string) func() {
	if x.aborter == nil {
		return nil
	}
	aborter := x.aborter
	return func() {
		_ = aborter.AbortByCallLegDeath(context.Background(), workspaceID, callID)
	}
}

var _ dialer_domain.TransferExecutor = (*dialerTransferExecutor)(nil)

// --- hold audio ------------------------------------------------------------

func (x *dialerTransferExecutor) newHoldSource(workspaceID string) dialer_infra.HoldAudioSource {
	if x.holdAudio != nil {
		if src := x.holdAudio(workspaceID); src != nil {
			return src
		}
	}
	return dialer_infra.NewComfortToneSource()
}

// holdLeg starts hold audio on the caller leg and gates BOTH audio directions
// (holdActive suppresses the downlink dispatch and the WS handler's mic uplink),
// making the hold player the leg's sole writer. Idempotent.
func (x *dialerTransferExecutor) holdLeg(lc *liveDialerCall) {
	if lc == nil || lc.holdActive.Load() {
		return
	}
	player := dialer_infra.NewHoldPlayer(lc.call, x.newHoldSource(lc.workspaceID))
	player.Start(context.Background())
	if old := lc.holdPlayer.Swap(player); old != nil {
		old.Stop()
	}
	lc.holdActive.Store(true)
}

// releaseHoldLeg resumes normal two-way audio. Idempotent; the atomic swap makes
// concurrent releases stop the player exactly once.
func (x *dialerTransferExecutor) releaseHoldLeg(lc *liveDialerCall) {
	if lc == nil {
		return
	}
	lc.holdActive.Store(false)
	if p := lc.holdPlayer.Swap(nil); p != nil {
		p.Stop()
	}
}

func (x *dialerTransferExecutor) initiatorSession(h *dialer_domain.TransferHandle) (*dialerSession, error) {
	// Resolve the session that actually OWNS the call, not the member's preferred
	// contact. A member can hold a call on their browser while ALSO having a branch
	// (SIP phone) registered; FindByUser prefers the physical branch, which is not a
	// browser delivery session and does not hold the leg — so a transfer initiated
	// from the browser would fail with "initiator is not a delivery dialer session"
	// (e.g. accept a transfer on the browser, then transfer back). The call registry
	// records the true owner session, so key on it — the same "hit the exact contact
	// that owns the leg" principle resolveSessionForUser uses for the target.
	if entry, ok := x.calls.Lookup(h.WorkspaceID, h.CallID); ok && entry.OwnerSessionID != "" {
		for _, s := range x.sessions.FindSessionsByUser(h.WorkspaceID, h.InitiatorID) {
			if ds, isDS := s.(*dialerSession); isDS && ds.ID() == entry.OwnerSessionID {
				return ds, nil
			}
		}
	}

	// Fallback: the member's preferred contact (paths with no call entry yet).
	initiatorIface, ok := x.sessions.FindByUser(h.WorkspaceID, h.InitiatorID)
	if !ok {
		return nil, &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferNotFound,
			Reason: string(dialer_domain.TransferReasonInitiatorDisconnect),
		}
	}
	initiator, ok := initiatorIface.(*dialerSession)
	if !ok {
		return nil, errors.New("transfer executor: initiator is not a delivery dialer session")
	}
	return initiator, nil
}

func (x *dialerTransferExecutor) HoldCaller(ctx context.Context, h *dialer_domain.TransferHandle) error {
	if h == nil {
		return errors.New("nil transfer handle")
	}
	initiator, err := x.initiatorSession(h)
	if err != nil {
		return err
	}
	lc := initiator.Current()
	if lc == nil || lc.call.ID() != h.CallID {
		return dialer_domain.ErrTransferCallNotFound
	}
	x.holdLeg(lc)
	return nil
}

func (x *dialerTransferExecutor) ReleaseHold(ctx context.Context, h *dialer_domain.TransferHandle) {
	if h == nil {
		return
	}
	initiator, err := x.initiatorSession(h)
	if err != nil {
		return
	}
	lc := initiator.Current()
	if lc == nil || lc.call.ID() != h.CallID {
		return
	}
	x.releaseHoldLeg(lc)
}

// --- park (true blind / blond / recall) -------------------------------------

func parkOwnerSessionID(transferID string) string { return "park:" + transferID }

func (x *dialerTransferExecutor) ParkBlind(ctx context.Context, h *dialer_domain.TransferHandle) error {
	if h == nil {
		return errors.New("nil transfer handle")
	}
	if x.park == nil {
		return errors.New("transfer executor: park registry not wired")
	}

	entry, ok := x.calls.Lookup(h.WorkspaceID, h.CallID)
	if !ok {
		return dialer_domain.ErrTransferCallNotFound
	}
	initiator, err := x.initiatorSession(h)
	if err != nil {
		return err
	}
	lc := initiator.Current()
	if lc == nil || lc.call.ID() != h.CallID {
		return dialer_domain.ErrTransferCallNotFound
	}

	// Hold BEFORE detaching so there is no instant of dead air, then free the
	// initiator: from here the park owns the leg and the initiator can take the
	// next call. Every step unwinds on failure.
	x.holdLeg(lc)
	detached, ok := initiator.Detach()
	if !ok || detached != lc {
		x.releaseHoldLeg(lc)
		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferNotFound,
			Reason: string(dialer_domain.TransferReasonInitiatorDisconnect),
		}
	}
	if err := x.calls.Rebind(h.WorkspaceID, h.CallID, entry.OwnerSessionID, parkOwnerSessionID(h.ID), h.InitiatorID); err != nil {
		if reErr := initiator.Attach(lc); reErr != nil {
			x.releaseHoldLeg(lc)
			return errors.Join(err, reErr)
		}
		x.releaseHoldLeg(lc)
		return err
	}
	if err := x.park.Park(h.WorkspaceID, lc, h.ID, x.legDeathHook(h.WorkspaceID, h.CallID)); err != nil {
		if rbErr := x.calls.Rebind(h.WorkspaceID, h.CallID, parkOwnerSessionID(h.ID), initiator.ID(), initiator.UserID()); rbErr != nil {
			x.logger.Printf("[Transfer] park rollback rebind failed for %s: %v", h.ID, rbErr)
		}
		if reErr := initiator.Attach(lc); reErr != nil {
			x.releaseHoldLeg(lc)
			return errors.Join(err, reErr)
		}
		x.releaseHoldLeg(lc)
		return err
	}

	// The initiator's call view ends now: the call is transferred from their
	// perspective, whatever happens next (accept / recall / fallback).
	_ = initiator.Notify(dialer_domain.DialerControlMessage{
		Type: "call:ended",
		Payload: map[string]any{
			"call_id":      h.CallID,
			"phone_number": lc.phone,
			"reason":       "transferred",
		},
	})
	return nil
}

// SwapParked retrieves the parked leg and attaches it to toUserID's winning
// contact (the recall accepter or the transfer target). On failure the leg is
// re-parked, hold audio restored, so the recall ladder can continue.
func (x *dialerTransferExecutor) SwapParked(ctx context.Context, h *dialer_domain.TransferHandle, toUserID string) error {
	if h == nil {
		return errors.New("nil transfer handle")
	}
	if x.park == nil {
		return errors.New("transfer executor: park registry not wired")
	}

	targetIface := x.resolveSessionForUser(h, toUserID)
	if targetIface == nil {
		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferTargetOffline,
			Reason: string(dialer_domain.TransferReasonTargetDisconnect),
		}
	}
	sink, ok := targetIface.(dialer_domain.CallLegSink)
	if !ok {
		return errors.New("transfer executor: target cannot receive a call leg")
	}
	if targetIface.ActiveCallID() != "" {
		return dialer_domain.ErrTransferTargetBusy
	}

	legIface, ok := x.park.Retrieve(h.WorkspaceID, h.CallID, h.ID)
	if !ok {
		// The caller hung up (death watcher removed it) or a competing terminal won.
		return dialer_domain.ErrTransferCallNotFound
	}

	repark := func(reason string, cause error) error {
		if err := x.park.Park(h.WorkspaceID, legIface, h.ID, x.legDeathHook(h.WorkspaceID, h.CallID)); err != nil {
			x.logger.Printf("[Transfer] re-park failed for %s (%s): %v — hanging the leg up", h.ID, reason, err)
			_ = legIface.Hangup()
		}
		return cause
	}

	if err := x.calls.Rebind(h.WorkspaceID, h.CallID, parkOwnerSessionID(h.ID), targetIface.ID(), targetIface.UserID()); err != nil {
		return repark("rebind", err)
	}

	// Stop the hold audio right before the attach: the hold player must never
	// write concurrently with a media surrender (branch targets), and the new
	// owner hears the live caller immediately.
	if lc, isLive := legIface.(*liveDialerCall); isLive {
		x.releaseHoldLeg(lc)
	}

	if err := sink.AttachLeg(legIface); err != nil {
		if rbErr := x.calls.Rebind(h.WorkspaceID, h.CallID, targetIface.ID(), parkOwnerSessionID(h.ID), h.InitiatorID); rbErr != nil {
			x.logger.Printf("[Transfer] attach rollback rebind failed for %s: %v", h.ID, rbErr)
		}
		if lc, isLive := legIface.(*liveDialerCall); isLive {
			x.holdLeg(lc)
		}
		return repark("attach", err)
	}

	_ = targetIface.Notify(dialer_domain.DialerControlMessage{
		Type: "transfer:started",
		Payload: map[string]any{
			"transfer_id":  h.ID,
			"call_id":      h.CallID,
			"workspace_id": h.WorkspaceID,
			"initiator_id": h.InitiatorID,
			"kind":         string(h.Kind),
			"phone_number": legIface.PhoneNumber(),
			"recall":       toUserID == h.InitiatorID,
		},
	})
	return nil
}

// AbandonPark is the fallback terminal: nobody could take the parked caller, so
// play the fallback announcement (when configured) and hang up. Never leaves a
// caller in infinite hold audio (rule R4).
func (x *dialerTransferExecutor) AbandonPark(ctx context.Context, h *dialer_domain.TransferHandle, reason string) {
	if h == nil || x.park == nil {
		return
	}
	legIface, ok := x.park.Abandon(h.WorkspaceID, h.CallID, h.ID)
	if !ok {
		return // already retrieved, or the death watcher removed it
	}
	lc, isLive := legIface.(*liveDialerCall)
	if isLive {
		x.releaseHoldLeg(lc)
	}
	x.logger.Printf("[Transfer] abandoning parked call %s (transfer %s, %s)", h.CallID, h.ID, reason)

	if len(x.fallbackPCM) == 0 || !isLive {
		_ = legIface.Hangup()
		return
	}
	// Play the announcement once, then hang up. The player loops, so bound it by
	// the asset's real duration (bytes / 16000 per second at 8kHz 16-bit mono).
	pcm := x.fallbackPCM
	duration := time.Duration(len(pcm)/2) * time.Second / 8000
	player := dialer_infra.NewHoldPlayer(lc.call, dialer_infra.NewLoopingPCMSource(pcm))
	player.Start(context.Background())
	go func() {
		defer func() { _ = legIface.Hangup() }()
		select {
		case <-time.After(duration + 100*time.Millisecond):
		case <-legIface.Done():
		}
		player.Stop()
	}()
}

// resolveSessionForUser returns the contact of userID that won the CURRENT ring
// wave (AcceptedSessionID), falling back to the member's preferred contact.
func (x *dialerTransferExecutor) resolveSessionForUser(h *dialer_domain.TransferHandle, userID string) dialer_domain.DialerSession {
	if h.AcceptedSessionID != "" {
		for _, s := range x.sessions.FindSessionsByUser(h.WorkspaceID, userID) {
			if s != nil && s.ID() == h.AcceptedSessionID {
				return s
			}
		}
	}
	if s, ok := x.sessions.FindByUser(h.WorkspaceID, userID); ok {
		return s
	}
	return nil
}

// resolveTargetSession returns the exact contact that WON the offer (the one whose
// id was recorded as AcceptedSessionID when it accepted), not merely the member's
// preferred contact. This is what makes a forked offer land on the endpoint that
// actually answered (the browser or the branch), not the other one.
func (x *dialerTransferExecutor) resolveTargetSession(h *dialer_domain.TransferHandle) (dialer_domain.DialerSession, bool) {
	if h == nil {
		return nil, false
	}
	if s := x.resolveSessionForUser(h, h.TargetUserID); s != nil {
		return s, true
	}
	return nil, false
}

func (x *dialerTransferExecutor) SwapBlind(ctx context.Context, h *dialer_domain.TransferHandle) error {
	if h == nil {
		return errors.New("nil transfer handle")
	}

	// True-blind and blond handles parked the leg at initiate/complete: the swap
	// is a park retrieve, not a detach from the initiator (who is long free).
	if x.park != nil {
		if _, parked := x.park.Find(h.WorkspaceID, h.CallID); parked || h.Parked {
			return x.SwapParked(ctx, h, h.TargetUserID)
		}
	}

	entry, ok := x.calls.Lookup(h.WorkspaceID, h.CallID)
	if !ok {
		return dialer_domain.ErrTransferCallNotFound
	}

	initiator, err := x.initiatorSession(h)
	if err != nil {
		return err
	}

	targetIface, ok := x.resolveTargetSession(h)
	if !ok {
		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferTargetOffline,
			Reason: string(dialer_domain.TransferReasonTargetDisconnect),
		}
	}
	// The target may be a browser session or a branch: both implement CallLegSink, so
	// the swap FSM below is identical and the media specifics (forwarder re-point vs
	// RTP bridge) live behind AttachLeg. This generalization is what lets a branch be
	// a first-class transfer target with no duplication of this rebind/attach dance.
	target, ok := targetIface.(dialer_domain.CallLegSink)
	if !ok {
		return errors.New("transfer executor: target cannot receive a call leg")
	}
	// Reject only when the target holds a genuine attached call. A bare
	// reservation here is this transfer's own ring (taken at Initiate and consumed
	// by AttachLeg below), so it must not register as "busy" — that's why we check
	// ActiveCallID rather than the reservation-aware HasActiveCall.
	if targetIface.ActiveCallID() != "" {
		return dialer_domain.ErrTransferTargetBusy
	}

	lc, detached := initiator.Detach()
	if !detached {

		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferNotFound,
			Reason: string(dialer_domain.TransferReasonInitiatorDisconnect),
		}
	}

	// A held caller (attended flows that fall through here) resumes normal audio
	// with their NEW owner, never MOH into the swap.
	x.releaseHoldLeg(lc)

	if err := x.calls.Rebind(h.WorkspaceID, h.CallID, entry.OwnerSessionID, targetIface.ID(), targetIface.UserID()); err != nil {

		if reErr := initiator.Attach(lc); reErr != nil {

			return errors.Join(err, reErr)
		}
		return err
	}

	if err := target.AttachLeg(lc); err != nil {
		if rbErr := x.calls.Rebind(h.WorkspaceID, h.CallID, targetIface.ID(), initiator.ID(), initiator.UserID()); rbErr != nil {

			return errors.Join(err, rbErr)
		}
		if reErr := initiator.Attach(lc); reErr != nil {
			return errors.Join(err, reErr)
		}
		return err
	}

	_ = targetIface.Notify(dialer_domain.DialerControlMessage{
		Type: "transfer:started",
		Payload: map[string]any{
			"transfer_id":  h.ID,
			"call_id":      h.CallID,
			"workspace_id": h.WorkspaceID,
			"initiator_id": h.InitiatorID,
			"kind":         string(h.Kind),
			"phone_number": lc.phone,
		},
	})
	return nil
}

func (x *dialerTransferExecutor) OpenConsult(ctx context.Context, h *dialer_domain.TransferHandle) error {
	if h == nil {
		return errors.New("nil transfer handle")
	}

	initiator, targetSess, targetConsult, lc, err := x.resolveConsultParticipants(h)
	if err != nil {
		return err
	}

	bridge := dialer_infra.NewConsultBridge(0)

	// Wire the disconnect hooks BEFORE attaching media, so a phone hangup the
	// instant the branch consult relay starts still aborts the transfer (the branch
	// reads its hook live). For a browser target this ordering is harmless.
	if hook := x.roleHook(string(dialer_domain.TransferReasonInitiatorDisconnect)); hook != nil {
		initiator.SetTransferContext(h.ID, hook)
	}
	if hook := x.roleHook(string(dialer_domain.TransferReasonTargetDisconnect)); hook != nil {
		targetConsult.SetTransferContext(h.ID, hook)
	}

	if err := initiator.AttachConsult(bridge.A()); err != nil {
		initiator.ClearTransferContext()
		targetConsult.ClearTransferContext()
		bridge.Close()
		return err
	}
	if err := targetConsult.AttachConsult(bridge.B()); err != nil {
		initiator.DetachConsult()
		initiator.ClearTransferContext()
		targetConsult.ClearTransferContext()
		bridge.Close()
		return err
	}

	// The caller has been on hold audio since Initiate (HoldCaller); this is a
	// defensive, idempotent re-assert for handles that predate that flow.
	x.holdLeg(lc)

	_ = initiator.Notify(dialer_domain.DialerControlMessage{
		Type: "transfer:consulting",
		Payload: map[string]any{
			"transfer_id":    h.ID,
			"call_id":        h.CallID,
			"target_user_id": h.TargetUserID,
			"role":           "initiator",
		},
	})
	_ = targetSess.Notify(dialer_domain.DialerControlMessage{
		Type: "transfer:consulting",
		Payload: map[string]any{
			"transfer_id":  h.ID,
			"call_id":      h.CallID,
			"initiator_id": h.InitiatorID,
			"role":         "target",
		},
	})
	return nil
}

func (x *dialerTransferExecutor) CloseConsult(ctx context.Context, h *dialer_domain.TransferHandle, completed bool) error {
	if h == nil {
		return errors.New("nil transfer handle")
	}

	initiator, targetSess, targetConsult, lc := x.lookupConsultParticipants(h)

	if initiator != nil {
		initiator.ClearTransferContext()
	}
	if targetConsult != nil {
		targetConsult.ClearTransferContext()
	}

	if initiator != nil {
		initiator.DetachConsult()
	}
	if targetConsult != nil {
		targetConsult.DetachConsult()
	}

	if !completed {
		// The consult ends and the caller resumes with the initiator: off hold.
		if lc != nil {
			x.releaseHoldLeg(lc)
		}
		// A branch target must hang its answered phone up on a cancelled consult
		// (DetachConsult only stopped the transcode relay; the phone dialog is still
		// up). AbortConsult BYEs it. A browser target does not implement it — no-op.
		if ab, ok := targetSess.(consultAborter); ok {
			ab.AbortConsult()
		}
		if initiator != nil {
			_ = initiator.Notify(dialer_domain.DialerControlMessage{
				Type:    "transfer:cancelled",
				Payload: map[string]any{"transfer_id": h.ID, "call_id": h.CallID, "role": "initiator"},
			})
		}
		if targetSess != nil {
			_ = targetSess.Notify(dialer_domain.DialerControlMessage{
				Type:    "transfer:cancelled",
				Payload: map[string]any{"transfer_id": h.ID, "call_id": h.CallID, "role": "target"},
			})
		}
		return nil
	}

	if initiator == nil {
		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferNotFound,
			Reason: string(dialer_domain.TransferReasonInitiatorDisconnect),
		}
	}
	if targetSess == nil {
		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferTargetOffline,
			Reason: string(dialer_domain.TransferReasonTargetDisconnect),
		}
	}
	if lc == nil {
		return dialer_domain.ErrTransferCallNotFound
	}
	sink, ok := targetSess.(dialer_domain.CallLegSink)
	if !ok {
		return errors.New("transfer executor: target cannot receive a call leg")
	}

	entry, ok := x.calls.Lookup(h.WorkspaceID, h.CallID)
	if !ok {
		return dialer_domain.ErrTransferCallNotFound
	}
	detached, ok := initiator.Detach()
	if !ok || detached != lc {
		return &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferNotFound,
			Reason: string(dialer_domain.TransferReasonInitiatorDisconnect),
		}
	}

	// Off hold right before the attach: the new owner hears the live caller.
	x.releaseHoldLeg(lc)

	if err := x.calls.Rebind(h.WorkspaceID, h.CallID, entry.OwnerSessionID, targetSess.ID(), targetSess.UserID()); err != nil {
		if reErr := initiator.Attach(lc); reErr != nil {
			return errors.Join(err, reErr)
		}
		x.holdLeg(lc) // still consulting from the caller's perspective
		return err
	}
	if err := sink.AttachLeg(lc); err != nil {
		if rbErr := x.calls.Rebind(h.WorkspaceID, h.CallID, targetSess.ID(), initiator.ID(), initiator.UserID()); rbErr != nil {

			return errors.Join(err, rbErr)
		}
		if reErr := initiator.Attach(lc); reErr != nil {
			return errors.Join(err, reErr)
		}
		x.holdLeg(lc)
		return err
	}

	_ = targetSess.Notify(dialer_domain.DialerControlMessage{
		Type: "transfer:started",
		Payload: map[string]any{
			"transfer_id":  h.ID,
			"call_id":      h.CallID,
			"workspace_id": h.WorkspaceID,
			"initiator_id": h.InitiatorID,
			"kind":         string(h.Kind),
			"phone_number": lc.phone,
		},
	})
	return nil
}

func (x *dialerTransferExecutor) resolveConsultParticipants(h *dialer_domain.TransferHandle) (*dialerSession, dialer_domain.DialerSession, consultCapable, *liveDialerCall, error) {
	initiator, err := x.initiatorSession(h)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	targetIface, ok := x.resolveTargetSession(h)
	if !ok {
		return nil, nil, nil, nil, &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferTargetOffline,
			Reason: string(dialer_domain.TransferReasonTargetDisconnect),
		}
	}
	targetConsult, ok := targetIface.(consultCapable)
	if !ok {
		return nil, nil, nil, nil, errors.New("transfer executor: target endpoint does not support attended consult")
	}

	lc := initiator.Current()
	if lc == nil || lc.call.ID() != h.CallID {
		return nil, nil, nil, nil, &dialer_domain.TransferError{
			Err:    dialer_domain.ErrTransferNotFound,
			Reason: string(dialer_domain.TransferReasonInitiatorDisconnect),
		}
	}
	return initiator, targetIface, targetConsult, lc, nil
}

func (x *dialerTransferExecutor) lookupConsultParticipants(h *dialer_domain.TransferHandle) (*dialerSession, dialer_domain.DialerSession, consultCapable, *liveDialerCall) {
	var (
		initiator     *dialerSession
		targetSess    dialer_domain.DialerSession
		targetConsult consultCapable
		lc            *liveDialerCall
	)

	if iface, ok := x.sessions.FindByUser(h.WorkspaceID, h.InitiatorID); ok {
		if s, ok := iface.(*dialerSession); ok {
			initiator = s
			if cur := s.Current(); cur != nil && cur.call.ID() == h.CallID {
				lc = cur
			}
		}
	}
	if iface, ok := x.resolveTargetSession(h); ok {
		targetSess = iface
		if tc, ok := iface.(consultCapable); ok {
			targetConsult = tc
		}
	}
	return initiator, targetSess, targetConsult, lc
}
