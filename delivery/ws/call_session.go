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
	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
	callsession_usecase "vozko/usecases/callsession"
)

type liveCall struct {
	call        conversation.CRMCall
	admission   *callsession_domain.CallAdmissionLease
	phone       string
	requestID   string
	workspaceID string
	ownerUserID string
	startedAt   time.Time
	direction   cdr.Direction

	forwarder     atomic.Pointer[callSession]
	lifecycleDone chan struct{}
	started       atomic.Bool

	uplink         chan []byte
	uplinkClose    sync.Once
	uplinkDropped  atomic.Uint64
	uplinkStarted  atomic.Bool
	uplinkDoneOnce sync.Once
	uplinkDone     chan struct{}

	inboundConverter inboundAudioConverter
}

const callSessionUplinkQueueDepth = 32

func (lc *liveCall) startUplinkPump(logger *log.Logger) {
	if !lc.uplinkStarted.CompareAndSwap(false, true) {
		return
	}
	lc.uplink = make(chan []byte, callSessionUplinkQueueDepth)
	lc.uplinkDone = make(chan struct{})
	go func() {
		defer close(lc.uplinkDone)
		var processed uint64
		for pcm := range lc.uplink {
			if err := lc.call.SendAudio(pcm); err != nil {
				if logger != nil {
					logger.Printf("[CallSessionWS] SendAudio failed for call %s: %v", lc.call.ID(), err)
				}
			}
			// Periodic uplink heartbeat: channel-level drops (producer outpacing
			// the real-time send) + current queue depth. A climbing drop count or a
			// persistently full queue means browser audio is arriving faster than it
			// can be sent, the buffer-bloat signature behind growing uplink delay.
			processed++
			if processed%500 == 0 && logger != nil {
				logger.Printf("[CallSessionWS] uplink pump call=%s processed=%d channel_drops=%d queue=%d/%d",
					lc.call.ID(), processed, lc.uplinkDropped.Load(), len(lc.uplink), cap(lc.uplink))
			}
		}
	}()
}

func (lc *liveCall) enqueueAudio(pcm []byte) bool {
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

func (lc *liveCall) stopUplinkPump() {
	lc.uplinkClose.Do(func() {
		if lc.uplink != nil {
			close(lc.uplink)
		}
	})
	if lc.uplinkDone != nil {
		<-lc.uplinkDone
	}
}

func (lc *liveCall) start(
	ctx context.Context,
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner,
	endUseCase callsession_domain.EndOutboundCallUseCase,
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
				_ = endUseCase.Execute(context.Background(), callsession_domain.EndOutboundCallInput{
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
		lifecycle.Run(ctx, callsession_usecase.OutboundCallLifecycleInput{
			Call:        lc.call,
			Admission:   lc.admission,
			WorkspaceID: lc.workspaceID,
			OwnerUserID: ownerUserID,
			StartedAt:   lc.startedAt,
			Direction:   lc.direction,
			PhoneTo:     lc.phone,
			OnStatus: func(event conversation.CallEvent) {
				if s := lc.forwarder.Load(); s != nil {
					s.dispatchStatus(lc, event)
				}
			},
			OnAudio: func(pcm []byte) {
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

func (lc *liveCall) done() <-chan struct{} { return lc.lifecycleDone }

type callSession struct {
	id                    string
	userID                string
	workspaceID           string
	send                  func(*WSOutgoingMessage)
	logger                *log.Logger
	endUseCase            callsession_domain.EndOutboundCallUseCase
	forcedShutdownTimeout time.Duration

	mu      sync.Mutex
	current *liveCall

	// res is the busy-while-ringing reservation, shared with every other
	// CallSession implementation via the domain ReservationState primitive so
	// the compare-and-set + TTL logic is not duplicated per session type. It is
	// guarded by mu together with current, so accept transitions reserved->active
	// with no observable free gap; current is passed in as the "already active"
	// predicate to keep Reserve atomic under this single lock.
	res callsession_domain.ReservationState
	now func() time.Time

	onPresenceChange func()

	// presenceTelemetry records durable on_call/online intervals (queue only, optional).
	presenceTelemetry func(workspaceID, userID, state, source string)
}

var errCallSessionBusy = errors.New("call session already has an attached call")

// callSessionReservationTTL is the shared reservation backstop, single-sourced in the
// domain so every CallSession implementation uses the same window. See
// callsession_domain.CallSessionReservationTTL for the rationale.
const callSessionReservationTTL = callsession_domain.CallSessionReservationTTL

func newCallSession(
	id, userID, workspaceID string,
	send func(*WSOutgoingMessage),
	endUseCase callsession_domain.EndOutboundCallUseCase,
	logger *log.Logger,
	forcedShutdownTimeout time.Duration,
) *callSession {
	if logger == nil {
		logger = log.Default()
	}
	if forcedShutdownTimeout <= 0 {
		forcedShutdownTimeout = callSessionForcedShutdownTimeout
	}
	return &callSession{
		id:                    id,
		userID:                userID,
		workspaceID:           workspaceID,
		send:                  send,
		endUseCase:            endUseCase,
		logger:                logger,
		forcedShutdownTimeout: forcedShutdownTimeout,
		now:                   time.Now,
	}
}

func (s *callSession) Attach(lc *liveCall) error {
	if lc == nil {
		return errors.New("nil live call")
	}
	s.mu.Lock()
	if s.current != nil {
		s.mu.Unlock()
		return errCallSessionBusy
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
		tel(s.workspaceID, s.userID, "on_call", "call_session")
	}
	return nil
}

func (s *callSession) Detach() (*liveCall, bool) {
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
		tel(s.workspaceID, s.userID, "online", "call_session")
	}
	return lc, true
}

func (s *callSession) Current() *liveCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Done exposes the lifecycle-terminated signal: it closes when the far side hung
// up or the call was torn down.
func (lc *liveCall) Done() <-chan struct{} { return lc.lifecycleDone }

func (s *callSession) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isOccupiedLocked()
}

// Reserve marks this session as occupied for an outstanding ring identified by
// token. It is a compare-and-set: it fails (returns false) if the session
// already has an attached call or a live reservation for a different token, so
// two concurrent offers can never both claim the same idle agent. Reserving with
// the same token again is idempotent and returns true.
func (s *callSession) Reserve(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// current is the "already active" predicate, so the compare-and-set stays
	// atomic with attach under this single lock. Empty-token and TTL handling live
	// in the shared ReservationState.
	return s.res.Reserve(token, s.current != nil, s.now(), callSessionReservationTTL)
}

// Release clears a reservation taken with the same token. It is token-scoped and
// idempotent: releasing a stale or foreign token, after Attach already consumed
// the reservation (reserved == ""), or after the agent reconnected and a newer
// offer re-reserved the session, is a no-op, so duplicate/liberal releases from
// every resolution site are safe. Release never touches an attached call.
func (s *callSession) Release(token string) {
	s.mu.Lock()
	s.res.Release(token)
	s.mu.Unlock()
}

// clearReservation unconditionally drops any outstanding reservation. Used on
// session shutdown so an agent that disconnects while a ring is outstanding frees
// its slot immediately instead of waiting out the TTL backstop.
func (s *callSession) clearReservation() {
	s.mu.Lock()
	s.res.Clear()
	s.mu.Unlock()
}

// reservedLiveLocked reports whether a non-expired reservation is held, lazily
// clearing one that has outlived the TTL backstop. Caller must hold s.mu.
func (s *callSession) reservedLiveLocked() bool {
	return s.res.ReservedLive(s.now(), callSessionReservationTTL)
}

// isOccupiedLocked reports whether the session is unavailable for a new call,
// either it has an attached call or a live ring reservation. Caller holds s.mu.
func (s *callSession) isOccupiedLocked() bool {
	return s.current != nil || s.reservedLiveLocked()
}

func (s *callSession) ActiveCallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return ""
	}
	return s.current.call.ID()
}

func (s *callSession) ID() string          { return s.id }
func (s *callSession) UserID() string      { return s.userID }
func (s *callSession) WorkspaceID() string { return s.workspaceID }

func (s *callSession) SetPresenceCallback(cb func()) {
	if s == nil {
		return
	}
	s.onPresenceChange = cb
}

// SetPresenceTelemetry records durable on_call/online via queue-backed adapter.
func (s *callSession) SetPresenceTelemetry(fn func(workspaceID, userID, state, source string)) {
	if s == nil {
		return
	}
	s.presenceTelemetry = fn
}

func (s *callSession) Notify(msg callsession_domain.CallSessionControlMessage) error {
	if s == nil || s.send == nil {
		return errors.New("call session: no send function")
	}
	s.send(&WSOutgoingMessage{Type: WSEventType(msg.Type), Payload: msg.Payload})
	return nil
}

func (s *callSession) Shutdown(ctx context.Context) {

	// Release any outstanding ring reservation first: an agent that disconnects
	// while an offer is still ringing (before Attach) leaves current == nil, so
	// the call teardown below early-returns, without this the reservation would
	// linger until the TTL backstop. clearReservation is unconditional and safe
	// when no reservation is held.
	s.clearReservation()

	lc, ok := s.Detach()
	if !ok {
		return
	}

	if s.endUseCase != nil {
		_ = s.endUseCase.Execute(ctx, callsession_domain.EndOutboundCallInput{
			Call:   lc.call,
			Hangup: true,
		})
	}

	select {
	case <-lc.done():

	case <-time.After(s.forcedShutdownTimeout):
		s.logger.Printf("[CallSessionWS] lifecycle stuck after %s for call %s, forcing admission release to prevent slot leak",
			s.forcedShutdownTimeout, lc.call.ID())
		if s.endUseCase != nil {
			_ = s.endUseCase.Execute(ctx, callsession_domain.EndOutboundCallInput{
				Admission:        lc.admission,
				ReleaseAdmission: true,
			})
		} else if lc.admission != nil {

			s.logger.Printf("[CallSessionWS] CRITICAL: cannot force-release admission for call %s, endUseCase is nil. Slot may leak until admission TTL expires.", lc.call.ID())
		}
	}
}

func (s *callSession) dispatchStatus(lc *liveCall, event conversation.CallEvent) {
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

func (s *callSession) dispatchAudio(pcm []byte) {
	s.send(&WSOutgoingMessage{
		Type: WSEventCallAudioS,
		Payload: CallAudioOutPayload{
			Audio:      base64.StdEncoding.EncodeToString(pcm),
			SampleRate: sipDefaultSampleRate,
		},
	})
}

func (s *callSession) dispatchEnded(lc *liveCall, reason string, duration time.Duration) {
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

var _ callsession_domain.CallSession = (*callSession)(nil)
