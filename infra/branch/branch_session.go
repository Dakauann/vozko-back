package branchinfra

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"vozko/domain/branch"
	dialer_domain "vozko/domain/dialer"
	dialer_infra "vozko/infra/dialer"
)

// branchSession makes a registered branch a first-class dialer_domain.DialerSession
// so the EXISTING transfer / roulette / offer-broker machinery routes to a SIP
// phone with no new transfer logic: FindByUser resolves it, Reserve gates it, and
// Notify(transfer:offer) rings it. Busy / double-ring uses the shared
// dialer_domain.ReservationState, identical to the browser dialerSession, so the
// compare-and-set + TTL logic is not duplicated.
type branchSession struct {
	id          string
	branchID    string
	sipUser     string
	userID      string
	workspaceID string
	ring        branch.BranchRing
	bridge      branch.BranchMediaBridge
	logger      *log.Logger
	now         func() time.Time

	mu           sync.Mutex
	res          dialer_domain.ReservationState
	activeCallID string
	stopBridge   func()

	// Attended-transfer consult state. consultStop ends the G.711<->PCM transcode
	// relay to the initiating agent; consultTransferID/consultAborter carry the
	// disconnect hook so a phone hangup mid-consult aborts the transfer as a
	// target-disconnect (the agent resumes with the caller).
	consultStop       func()
	consultTransferID string
	consultAborter    func(transferID, reason string)

	// onPresenceChange broadcasts a presence refresh when this branch's busy state
	// flips (answered / hung up), mirroring the browser session. Without it, a
	// branch going busy or free is only reflected when some other event happens to
	// trigger a broadcast.
	onPresenceChange func()

	// ringEligible is the member's "ring channels" policy applied to this branch
	// endpoint: true means this SIP extension may be rung. Defaults to true, synced
	// from the member's selection on Register and updated live on change.
	ringEligible atomic.Bool
}

// RingEligible reports whether this branch may be rung (optional registry interface).
func (s *branchSession) RingEligible() bool { return s.ringEligible.Load() }

// SetRingEligible applies the member's ring-channel selection to this endpoint.
func (s *branchSession) SetRingEligible(v bool) { s.ringEligible.Store(v) }

// --- attended consult (delivery/ws consultCapable, structural) -----------
// A branch hosts the private consult leg of an attended transfer: the answered
// phone talks to the initiating agent (transcoded G.711<->PCM) while the caller
// waits on hold. On complete the executor calls AttachLeg (phone bridged to the
// caller); on cancel it calls DetachConsult then AbortConsult (phone hung up).

// AttachConsult starts the transcode relay between the answered phone and the
// agent's consult endpoint. The endpoint satisfies branch.ConsultPeer, so no
// delivery media type crosses into the registrar's media plane.
func (s *branchSession) AttachConsult(endpoint *dialer_infra.ConsultEndpoint) error {
	if endpoint == nil {
		return errors.New("branch session: nil consult endpoint")
	}
	if s.bridge == nil {
		return errors.New("branch session: no media bridge configured")
	}
	stop, err := s.bridge.StartConsultBridge(s.branchID, endpoint, s.fireTargetDisconnect)
	if err != nil {
		return fmt.Errorf("branch session: start consult bridge: %w", err)
	}
	s.mu.Lock()
	s.consultStop = stop
	s.mu.Unlock()
	s.logger.Printf("[branch-session] %s consulting with the initiating agent", s.sipUser)
	return nil
}

// DetachConsult ends the transcode relay (synchronously) but LEAVES the phone leg
// parked: an attended completion reuses it for the caller bridge. On a cancel the
// executor follows with AbortConsult to hang the phone up.
func (s *branchSession) DetachConsult() {
	s.mu.Lock()
	stop := s.consultStop
	s.consultStop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// AbortConsult hangs up the parked phone after a cancelled consult. Idempotent:
// stops the relay if DetachConsult has not already, then BYEs the phone.
func (s *branchSession) AbortConsult() {
	s.mu.Lock()
	stop := s.consultStop
	s.consultStop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	if s.bridge != nil {
		s.bridge.CancelConsult(s.branchID)
	}
}

// SetTransferContext stores the disconnect hook the executor wires for the target
// side of the consult, so a phone hangup mid-consult aborts the transfer.
func (s *branchSession) SetTransferContext(transferID string, aborter func(transferID, reason string)) {
	s.mu.Lock()
	s.consultTransferID = transferID
	s.consultAborter = aborter
	s.mu.Unlock()
}

// ClearTransferContext drops the disconnect hook when the consult ends.
func (s *branchSession) ClearTransferContext() {
	s.mu.Lock()
	s.consultTransferID = ""
	s.consultAborter = nil
	s.mu.Unlock()
}

// fireTargetDisconnect is invoked by the consult relay when the phone hangs up
// during the consult: it drives the same target-disconnect abort a browser
// target's socket drop would (cancel the transfer, resume the caller with the
// agent). Reads the hook live so an ordering race at consult start is safe.
func (s *branchSession) fireTargetDisconnect() {
	s.mu.Lock()
	tid := s.consultTransferID
	ab := s.consultAborter
	s.mu.Unlock()
	if ab != nil && tid != "" {
		s.logger.Printf("[branch-session] %s: phone hung up during consult; aborting transfer %s", s.sipUser, tid)
		ab(tid, string(dialer_domain.TransferReasonTargetDisconnect))
	}
}

// SetPresenceCallback wires the registry's presence broadcast. Mirrors
// dialerSession.SetPresenceCallback so busy-state transitions push live.
func (s *branchSession) SetPresenceCallback(cb func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onPresenceChange = cb
	s.mu.Unlock()
}

// notifyPresence fires the callback outside the session lock (callers must not
// hold s.mu) so a re-entrant registry read cannot deadlock.
func (s *branchSession) notifyPresence() {
	s.mu.Lock()
	cb := s.onPresenceChange
	s.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func newBranchSession(b branch.RegisteredBranch, ring branch.BranchRing, bridge branch.BranchMediaBridge, logger *log.Logger) *branchSession {
	if logger == nil {
		logger = log.Default()
	}
	s := &branchSession{
		id:          "branch:" + b.BranchID,
		branchID:    b.BranchID,
		sipUser:     b.SIPUser,
		userID:      b.UserID,
		workspaceID: b.WorkspaceID,
		ring:        ring,
		bridge:      bridge,
		logger:      logger,
		now:         time.Now,
	}
	s.ringEligible.Store(true) // ring by default; the registry syncs the member's policy on Register
	return s
}

func (s *branchSession) ID() string          { return s.id }
func (s *branchSession) UserID() string      { return s.userID }
func (s *branchSession) WorkspaceID() string { return s.workspaceID }

// IsPhysicalEndpoint marks a branch as a registered device so the session registry
// prefers it over a transient browser session when a member holds both (until the
// transfer forks to every contact and rings them together).
func (s *branchSession) IsPhysicalEndpoint() bool { return true }

func (s *branchSession) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeCallID != "" || s.res.ReservedLive(s.now(), dialer_domain.DialerReservationTTL)
}

func (s *branchSession) ActiveCallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeCallID
}

// Reserve mirrors the browser session exactly, using the shared primitive with
// activeCallID as the "already active" predicate so the CAS stays atomic under
// this single lock.
func (s *branchSession) Reserve(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.res.Reserve(token, s.activeCallID != "", s.now(), dialer_domain.DialerReservationTTL)
}

func (s *branchSession) Release(token string) {
	s.mu.Lock()
	s.res.Release(token)
	s.mu.Unlock()
}

// markActive / clearActive are driven by the ring flow (SIP dialog) on 200 OK and
// BYE. markActive consumes any outstanding reservation atomically, exactly like
// the browser session's Attach.
func (s *branchSession) markActive(callID string) {
	s.mu.Lock()
	s.activeCallID = callID
	s.res.Clear()
	s.mu.Unlock()
	s.notifyPresence()
}

func (s *branchSession) clearActive() {
	s.mu.Lock()
	s.activeCallID = ""
	stop := s.stopBridge
	s.stopBridge = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	s.notifyPresence()
}

// --- dialer_domain.CallLegSink -------------------------------------------
// AttachLeg completes a blind transfer of the caller leg onto this branch: it takes
// exclusive raw access to the caller's media (SurrenderMedia) and hands it to the
// registrar's RTP bridge, which relays it to the already-answered phone dialog. This
// is what makes a phone a first-class transfer target — the browser session does the
// analogous forwarder re-point; the transfer FSM in the executor is shared, not
// duplicated (plan §5.4). DetachLeg is a no-op: a branch is a terminal target that
// never yields the leg back (re-transfer FROM a phone is a later REFER phase).

var _ dialer_domain.CallLegSink = (*branchSession)(nil)

func (s *branchSession) AttachLeg(leg dialer_domain.CallLeg) error {
	if leg == nil {
		return errors.New("branch session: nil call leg")
	}
	if s.bridge == nil {
		return errors.New("branch session: no media bridge configured")
	}

	caller, err := leg.SurrenderMedia()
	if err != nil {
		return fmt.Errorf("branch session: surrender caller media: %w", err)
	}

	// onEnd fires when the phone hangs up: end the caller leg (which unregisters the
	// call via its own lifecycle) and clear this session so it is offerable again.
	onEnd := func() {
		_ = leg.Hangup()
		s.clearActive()
	}
	stop, err := s.bridge.StartBridge(s.branchID, leg.CallID(), caller, onEnd)
	if err != nil {
		_ = leg.Hangup()
		return fmt.Errorf("branch session: start media bridge: %w", err)
	}

	s.mu.Lock()
	s.activeCallID = leg.CallID()
	s.stopBridge = stop
	s.res.Clear()
	s.mu.Unlock()
	s.notifyPresence()
	s.logger.Printf("[branch-session] %s bridged call %s to the branch phone", s.sipUser, leg.CallID())
	return nil
}

func (s *branchSession) DetachLeg() (dialer_domain.CallLeg, bool) {
	return nil, false
}

// Notify translates a routing control message into a real action for a SIP phone: an
// offer becomes a ring (SIP INVITE), and a cancellation (the call was answered on the
// member's other contact, or declined, or timed out) stops that ring (SIP CANCEL) so
// the phone does not keep ringing. Other WS-oriented control messages are no-ops.
func (s *branchSession) Notify(msg dialer_domain.DialerControlMessage) error {
	req := branch.BranchRingRequest{
		BranchID:    s.branchID,
		SIPUser:     s.sipUser,
		WorkspaceID: s.workspaceID,
		UserID:      s.userID,
	}
	if payload, ok := msg.Payload.(map[string]any); ok {
		if v, ok := payload["call_id"].(string); ok {
			req.CallID = v
		}
		if v, ok := payload["transfer_id"].(string); ok {
			req.TransferID = v
		}
		if v, ok := payload["initiator_id"].(string); ok {
			req.InitiatorID = v
		}
	}
	switch msg.Type {
	case "transfer:offer":
		if s.ring == nil {
			return errors.New("branch session: no ring gateway configured")
		}
		return s.ring.Ring(req)
	case "transfer:cancelled", "transfer:timed_out":
		if s.ring == nil {
			return nil
		}
		return s.ring.CancelRing(req)
	default:
		return nil
	}
}

var _ dialer_domain.DialerSession = (*branchSession)(nil)
