package ws

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	"vozko/domain/voip"
	dialer_infra "vozko/infra/dialer"
	dialer_usecase "vozko/usecases/dialer"
)

type liveDialerCall struct {
	call        conversation.CRMCall
	admission   *dialer_domain.CallAdmissionLease
	phone       string
	requestID   string
	workspaceID string
	ownerUserID string
	sipTrunkID  string
	startedAt   time.Time
	direction   cdr.Direction

	forwarder     atomic.Pointer[dialerSession]
	lifecycleDone chan struct{}
	started       atomic.Bool

	holdActive atomic.Bool
	holdPlayer atomic.Pointer[dialer_infra.SilencePlayer]

	uplink         chan []byte
	uplinkClose    sync.Once
	uplinkDropped  atomic.Uint64
	uplinkStarted  atomic.Bool
	uplinkDoneOnce sync.Once
	uplinkDone     chan struct{}

	inboundConverter inboundAudioConverter
}

const dialerUplinkQueueDepth = 32

func (lc *liveDialerCall) startUplinkPump(logger *log.Logger) {
	if !lc.uplinkStarted.CompareAndSwap(false, true) {
		return
	}
	lc.uplink = make(chan []byte, dialerUplinkQueueDepth)
	lc.uplinkDone = make(chan struct{})
	go func() {
		defer close(lc.uplinkDone)
		var processed uint64
		for pcm := range lc.uplink {
			if err := lc.call.SendAudio(pcm); err != nil {
				if logger != nil {
					logger.Printf("[DialerWS] SendAudio failed for call %s: %v", lc.call.ID(), err)
				}
			}
			// Periodic uplink heartbeat: channel-level drops (producer outpacing
			// the real-time send) + current queue depth. A climbing drop count or a
			// persistently full queue means browser audio is arriving faster than it
			// can be sent — the buffer-bloat signature behind growing uplink delay.
			processed++
			if processed%500 == 0 && logger != nil {
				logger.Printf("[DialerWS] uplink pump call=%s processed=%d channel_drops=%d queue=%d/%d",
					lc.call.ID(), processed, lc.uplinkDropped.Load(), len(lc.uplink), cap(lc.uplink))
			}
		}
	}()
}

func (lc *liveDialerCall) enqueueAudio(pcm []byte) bool {
	if lc == nil || lc.uplink == nil {
		return false
	}
	select {
	case lc.uplink <- pcm:
		return true
	default:
	}

	select {
	case <-lc.uplink:
		lc.uplinkDropped.Add(1)
	default:
	}
	select {
	case lc.uplink <- pcm:
		return true
	default:
		lc.uplinkDropped.Add(1)
		return false
	}
}

func (lc *liveDialerCall) stopUplinkPump() {
	lc.uplinkClose.Do(func() {
		if lc.uplink != nil {
			close(lc.uplink)
		}
	})
	if lc.uplinkDone != nil {
		<-lc.uplinkDone
	}
}

func (lc *liveDialerCall) start(
	ctx context.Context,
	lifecycle *dialer_usecase.OutboundCallLifecycleRunner,
	endUseCase dialer_domain.EndOutboundCallUseCase,
	logger *log.Logger,
	onEnd func(),
) {
	if !lc.started.CompareAndSwap(false, true) {
		return
	}

	lc.startUplinkPump(logger)
	if lifecycle == nil {

		go func() {
			defer close(lc.lifecycleDone)
			defer lc.stopUplinkPump()
			if lc.admission != nil && endUseCase != nil {
				_ = endUseCase.Execute(context.Background(), dialer_domain.EndOutboundCallInput{
					Admission:        lc.admission,
					ReleaseAdmission: true,
				})
			}
			if onEnd != nil {
				onEnd()
			}
		}()
		return
	}
	go func() {

		defer close(lc.lifecycleDone)
		defer lc.stopUplinkPump()
		defer func() {
			if onEnd != nil {
				onEnd()
			}
		}()
		ownerUserID := lc.ownerUserID
		if ownerUserID == "" {
			if s := lc.forwarder.Load(); s != nil {
				ownerUserID = s.userID
			}
		}
		lifecycle.Run(ctx, dialer_usecase.OutboundCallLifecycleInput{
			Call:        lc.call,
			Admission:   lc.admission,
			WorkspaceID: lc.workspaceID,
			OwnerUserID: ownerUserID,
			StartedAt:   lc.startedAt,
			Direction:   lc.direction,
			PhoneTo:     lc.phone,
			TrunkID:     lc.sipTrunkID,
			OnStatus: func(event conversation.CallEvent) {
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchStatus(lc, event)
				}
			},
			OnAudio: func(pcm []byte) {
				if lc.holdActive.Load() {
					return
				}
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchAudio(pcm)
				}
			},
			OnEnded: func(reason string, duration time.Duration) {
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchEnded(lc, reason, duration)
				}
			},
		})
	}()
}

func (lc *liveDialerCall) done() <-chan struct{} { return lc.lifecycleDone }

func (lc *liveDialerCall) startSurrendered(
	ctx context.Context,
	logger *log.Logger,
	onEnd func(),
) {
	if !lc.started.CompareAndSwap(false, true) {
		return
	}
	lc.startUplinkPump(logger)
	go func() {
		defer close(lc.lifecycleDone)
		defer lc.stopUplinkPump()
		defer func() {
			if onEnd != nil {
				onEnd()
			}
		}()
		call := lc.call
		audioCh := call.AudioStream()
		eventsCh := call.Events()
		startedAt := lc.startedAt
		if startedAt.IsZero() {
			startedAt = time.Now()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-call.Done():
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchEnded(lc, "ended", time.Since(startedAt))
				}
				return
			case ev, ok := <-eventsCh:
				if !ok {
					eventsCh = nil
					continue
				}
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchStatus(lc, ev)
				}
				if ev.IsTerminal() {
					if s := lc.forwarder.Load(); s != nil {
						s.dispatchEnded(lc, string(ev.Type), time.Since(startedAt))
					}
					return
				}
			case pcm, ok := <-audioCh:
				if !ok {
					audioCh = nil
					continue
				}
				if lc.holdActive.Load() {
					continue
				}
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchAudio(pcm)
				}
			}
		}
	}()
}

type dialerSession struct {
	id                    string
	userID                string
	workspaceID           string
	send                  func(*WSOutgoingMessage)
	logger                *log.Logger
	endUseCase            dialer_domain.EndOutboundCallUseCase
	forcedShutdownTimeout time.Duration

	mu      sync.Mutex
	current *liveDialerCall

	// res is the busy-while-ringing reservation, shared with every other
	// DialerSession implementation via the domain ReservationState primitive so
	// the compare-and-set + TTL logic is not duplicated per session type. It is
	// guarded by mu together with current, so accept transitions reserved->active
	// with no observable free gap; current is passed in as the "already active"
	// predicate to keep Reserve atomic under this single lock.
	res dialer_domain.ReservationState
	now func() time.Time

	consultEnd    atomic.Pointer[dialer_infra.ConsultEndpoint]
	consultCancel context.CancelFunc
	consultDone   chan struct{}

	activeTransferID atomic.Value
	transferAborter  func(transferID, reason string)

	onPresenceChange func()

	// presenceTelemetry records durable on_call/online intervals (queue only, optional).
	presenceTelemetry func(workspaceID, userID, state, source string)

	// ringEligible is the member's "ring channels" policy applied to THIS endpoint:
	// true means this browser session may be rung on offers/transfers. Defaults to
	// true (ring), set at registration from the member's selection and updated live
	// when the member changes it. Busy is orthogonal (see res/current).
	ringEligible atomic.Bool
}

// RingEligible reports whether this session may be rung. Part of the optional
// interface the session registry checks; absence means "always eligible".
func (s *dialerSession) RingEligible() bool { return s.ringEligible.Load() }

// SetRingEligible applies the member's ring-channel selection to this endpoint.
func (s *dialerSession) SetRingEligible(v bool) { s.ringEligible.Store(v) }

var errDialerSessionBusy = errors.New("dialer session already has an attached call")

// dialerReservationTTL is the shared reservation backstop, single-sourced in the
// domain so every DialerSession implementation uses the same window. See
// dialer_domain.DialerReservationTTL for the rationale.
const dialerReservationTTL = dialer_domain.DialerReservationTTL

func newDialerSession(
	id, userID, workspaceID string,
	send func(*WSOutgoingMessage),
	endUseCase dialer_domain.EndOutboundCallUseCase,
	logger *log.Logger,
	forcedShutdownTimeout time.Duration,
) *dialerSession {
	if logger == nil {
		logger = log.Default()
	}
	if forcedShutdownTimeout <= 0 {
		forcedShutdownTimeout = dialerForcedShutdownTimeout
	}
	s := &dialerSession{
		id:                    id,
		userID:                userID,
		workspaceID:           workspaceID,
		send:                  send,
		endUseCase:            endUseCase,
		logger:                logger,
		forcedShutdownTimeout: forcedShutdownTimeout,
		now:                   time.Now,
	}
	s.ringEligible.Store(true) // ring by default; the registry syncs the member's policy on Register
	return s
}

func (s *dialerSession) Attach(lc *liveDialerCall) error {
	if lc == nil {
		return errors.New("nil live call")
	}
	s.mu.Lock()
	if s.current != nil {
		s.mu.Unlock()
		return errDialerSessionBusy
	}
	s.current = lc
	// Accept consumes any outstanding ring reservation atomically: reserved->active
	// happens in the same critical section that sets current, so no concurrent
	// selector ever observes the accepting agent as momentarily free.
	s.res.Clear()
	s.mu.Unlock()
	lc.forwarder.Store(s)
	if cb := s.onPresenceChange; cb != nil {
		cb()
	}
	if tel := s.presenceTelemetry; tel != nil {
		tel(s.workspaceID, s.userID, "on_call", "dialer")
	}
	return nil
}

func (s *dialerSession) Detach() (*liveDialerCall, bool) {
	s.mu.Lock()
	lc := s.current
	s.current = nil
	s.mu.Unlock()
	if lc == nil {
		return nil, false
	}

	lc.forwarder.CompareAndSwap(s, nil)
	if cb := s.onPresenceChange; cb != nil {
		cb()
	}
	// Back to available (WS still connected) for occupancy accounting.
	if tel := s.presenceTelemetry; tel != nil {
		tel(s.workspaceID, s.userID, "online", "dialer")
	}
	return lc, true
}

func (s *dialerSession) Current() *liveDialerCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// --- dialer_domain.CallLeg (implemented by *liveDialerCall) --------------
// Exposing the concrete browser leg as a CallLeg lets the generalized transfer
// executor hand it to any CallLegSink target (a browser session or a branch) with no
// knowledge of which kind it is.

var _ dialer_domain.CallLeg = (*liveDialerCall)(nil)

func (lc *liveDialerCall) CallID() string      { return lc.call.ID() }
func (lc *liveDialerCall) PhoneNumber() string { return lc.phone }
func (lc *liveDialerCall) Hangup() error       { return lc.call.Hangup() }

// Done exposes the lifecycle-terminated signal to the park registry's death
// watcher (dialer_domain.CallLeg): it closes when the far side hung up or the
// call was torn down, whether the leg is attached, consulting or parked.
func (lc *liveDialerCall) Done() <-chan struct{} { return lc.lifecycleDone }

// SurrenderMedia delegates to the underlying CRM call when it can hand over its raw
// media session (a SIP-trunk or passthrough leg). A WebSocket-only leg cannot, and
// returns an error so a branch target fails the transfer cleanly rather than bridging
// silence.
func (lc *liveDialerCall) SurrenderMedia() (voip.MediaSession, error) {
	surrenderer, ok := lc.call.(interface {
		SurrenderMedia() (voip.MediaSession, error)
	})
	if !ok {
		return nil, errors.New("call leg has no surrenderable media")
	}
	return surrenderer.SurrenderMedia()
}

// --- dialer_domain.CallLegSink (implemented by *dialerSession) -----------
// A browser session receives a leg by re-pointing its audio forwarder (Attach) and
// yields it the same way (Detach). Generalizing the executor against this port is
// what lets a branch be a transfer target without duplicating the swap FSM (§5.4).

var _ dialer_domain.CallLegSink = (*dialerSession)(nil)

func (s *dialerSession) AttachLeg(leg dialer_domain.CallLeg) error {
	lc, ok := leg.(*liveDialerCall)
	if !ok {
		return errors.New("dialer session: unsupported call leg type")
	}
	return s.Attach(lc)
}

func (s *dialerSession) DetachLeg() (dialer_domain.CallLeg, bool) {
	lc, ok := s.Detach()
	if !ok {
		return nil, false
	}
	return lc, true
}

func (s *dialerSession) AttachConsult(endpoint *dialer_infra.ConsultEndpoint) error {
	if endpoint == nil {
		return errors.New("nil consult endpoint")
	}
	s.mu.Lock()
	if s.consultEnd.Load() != nil {
		s.mu.Unlock()
		return errDialerSessionBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.consultEnd.Store(endpoint)
	s.consultCancel = cancel
	s.consultDone = done
	s.mu.Unlock()

	go func() {
		defer close(done)
		recv := endpoint.Recv()
		for {
			select {
			case <-ctx.Done():
				return
			case pcm, ok := <-recv:
				if !ok {
					return
				}

				s.dispatchAudio(pcm)
			}
		}
	}()
	return nil
}

func (s *dialerSession) DetachConsult() {
	s.mu.Lock()
	cancel := s.consultCancel
	done := s.consultDone
	s.consultEnd.Store(nil)
	s.consultCancel = nil
	s.consultDone = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *dialerSession) ConsultEndpoint() *dialer_infra.ConsultEndpoint {
	if s == nil {
		return nil
	}
	return s.consultEnd.Load()
}

func (s *dialerSession) SetTransferContext(transferID string, aborter func(transferID, reason string)) {
	if s == nil {
		return
	}
	s.activeTransferID.Store(transferID)
	s.mu.Lock()
	s.transferAborter = aborter
	s.mu.Unlock()
}

// ClearTransferContext is exported as part of the consultCapable port so a
// non-browser consult endpoint (a branch) can satisfy the same contract.
func (s *dialerSession) ClearTransferContext() {
	if s == nil {
		return
	}
	s.activeTransferID.Store("")
	s.mu.Lock()
	s.transferAborter = nil
	s.mu.Unlock()
}

func (s *dialerSession) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isOccupiedLocked()
}

// Reserve marks this session as occupied for an outstanding ring identified by
// token. It is a compare-and-set: it fails (returns false) if the session
// already has an attached call or a live reservation for a different token, so
// two concurrent offers can never both claim the same idle agent. Reserving with
// the same token again is idempotent and returns true.
func (s *dialerSession) Reserve(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// current is the "already active" predicate, so the compare-and-set stays
	// atomic with attach under this single lock. Empty-token and TTL handling live
	// in the shared ReservationState.
	return s.res.Reserve(token, s.current != nil, s.now(), dialerReservationTTL)
}

// Release clears a reservation taken with the same token. It is token-scoped and
// idempotent: releasing a stale or foreign token — after Attach already consumed
// the reservation (reserved == ""), or after the agent reconnected and a newer
// offer re-reserved the session — is a no-op, so duplicate/liberal releases from
// every resolution site are safe. Release never touches an attached call.
func (s *dialerSession) Release(token string) {
	s.mu.Lock()
	s.res.Release(token)
	s.mu.Unlock()
}

// clearReservation unconditionally drops any outstanding reservation. Used on
// session shutdown so an agent that disconnects while a ring is outstanding frees
// its slot immediately instead of waiting out the TTL backstop.
func (s *dialerSession) clearReservation() {
	s.mu.Lock()
	s.res.Clear()
	s.mu.Unlock()
}

// reservedLiveLocked reports whether a non-expired reservation is held, lazily
// clearing one that has outlived the TTL backstop. Caller must hold s.mu.
func (s *dialerSession) reservedLiveLocked() bool {
	return s.res.ReservedLive(s.now(), dialerReservationTTL)
}

// isOccupiedLocked reports whether the session is unavailable for a new call —
// either it has an attached call or a live ring reservation. Caller holds s.mu.
func (s *dialerSession) isOccupiedLocked() bool {
	return s.current != nil || s.reservedLiveLocked()
}

func (s *dialerSession) ActiveCallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return ""
	}
	return s.current.call.ID()
}

func (s *dialerSession) ID() string          { return s.id }
func (s *dialerSession) UserID() string      { return s.userID }
func (s *dialerSession) WorkspaceID() string { return s.workspaceID }

func (s *dialerSession) SetPresenceCallback(cb func()) {
	if s == nil {
		return
	}
	s.onPresenceChange = cb
}

// SetPresenceTelemetry records durable on_call/online via queue-backed adapter.
func (s *dialerSession) SetPresenceTelemetry(fn func(workspaceID, userID, state, source string)) {
	if s == nil {
		return
	}
	s.presenceTelemetry = fn
}

func (s *dialerSession) Notify(msg dialer_domain.DialerControlMessage) error {
	if s == nil || s.send == nil {
		return errors.New("dialer session: no send function")
	}
	s.send(&WSOutgoingMessage{Type: WSEventType(msg.Type), Payload: msg.Payload})
	return nil
}

func (s *dialerSession) Shutdown(ctx context.Context) {

	// Release any outstanding ring reservation first: an agent that disconnects
	// while an offer is still ringing (before Attach) leaves current == nil, so
	// the call teardown below early-returns — without this the reservation would
	// linger until the TTL backstop. clearReservation is unconditional and safe
	// when no reservation is held.
	s.clearReservation()

	if v := s.activeTransferID.Load(); v != nil {
		if tid, _ := v.(string); tid != "" {
			s.mu.Lock()
			abort := s.transferAborter
			s.mu.Unlock()
			if abort != nil {

				abort(tid, "participant_disconnect")
			}
		}
	}

	s.DetachConsult()

	lc, ok := s.Detach()
	if !ok {
		return
	}

	if player := lc.holdPlayer.Swap(nil); player != nil {
		player.Stop()
	}
	lc.holdActive.Store(false)

	if s.endUseCase != nil {
		_ = s.endUseCase.Execute(ctx, dialer_domain.EndOutboundCallInput{
			Call:   lc.call,
			Hangup: true,
		})
	}

	select {
	case <-lc.done():

	case <-time.After(s.forcedShutdownTimeout):
		s.logger.Printf("[DialerWS] lifecycle stuck after %s for call %s — forcing admission release to prevent slot leak",
			s.forcedShutdownTimeout, lc.call.ID())
		if s.endUseCase != nil {
			_ = s.endUseCase.Execute(ctx, dialer_domain.EndOutboundCallInput{
				Admission:        lc.admission,
				ReleaseAdmission: true,
			})
		} else if lc.admission != nil {

			s.logger.Printf("[DialerWS] CRITICAL: cannot force-release admission for call %s — endUseCase is nil. Slot may leak until admission TTL expires.", lc.call.ID())
		}
	}
}

func (s *dialerSession) dispatchStatus(lc *liveDialerCall, event conversation.CallEvent) {
	s.send(&WSOutgoingMessage{
		Type: WSEventCallStatus,
		Payload: CallStatusPayload{
			Status:      event.Type,
			Reason:      event.Reason,
			CallID:      lc.call.ID(),
			PhoneNumber: lc.phone,
			RequestID:   lc.requestID,
		},
	})
}

func (s *dialerSession) dispatchAudio(pcm []byte) {
	s.send(&WSOutgoingMessage{
		Type: WSEventCallAudioS,
		Payload: CallAudioOutPayload{
			Audio:      base64.StdEncoding.EncodeToString(pcm),
			SampleRate: sipDefaultSampleRate,
		},
	})
}

func (s *dialerSession) dispatchEnded(lc *liveDialerCall, reason string, duration time.Duration) {
	s.send(&WSOutgoingMessage{
		Type: WSEventCallEnded,
		Payload: CallEndedPayload{
			CallID:          lc.call.ID(),
			Reason:          reason,
			PhoneNumber:     lc.phone,
			DurationSeconds: duration.Seconds(),
			RequestID:       lc.requestID,
		},
	})

	s.mu.Lock()
	cleared := s.current == lc
	if cleared {
		s.current = nil
	}
	s.mu.Unlock()
	lc.forwarder.CompareAndSwap(s, nil)

	// A natural call end frees this agent, so broadcast the presence change exactly
	// like Detach does. Without this, OTHER members' presence panels (and the
	// transfer picker) keep showing this agent as busy until some unrelated presence
	// event fires: "I ended my call but everyone still sees me busy".
	if cleared {
		if cb := s.onPresenceChange; cb != nil {
			cb()
		}
	}
}

var _ dialer_domain.DialerSession = (*dialerSession)(nil)
