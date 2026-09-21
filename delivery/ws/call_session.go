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

	res callsession_domain.ReservationState
	now func() time.Time

	onPresenceChange func()

	presenceTelemetry func(workspaceID, userID, state, source string)
}

var errCallSessionBusy = errors.New("call session already has an attached call")

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

func (lc *liveCall) Done() <-chan struct{} { return lc.lifecycleDone }

func (s *callSession) HasActiveCall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isOccupiedLocked()
}

func (s *callSession) Reserve(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.res.Reserve(token, s.current != nil, s.now(), callSessionReservationTTL)
}

func (s *callSession) Release(token string) {
	s.mu.Lock()
	s.res.Release(token)
	s.mu.Unlock()
}

func (s *callSession) clearReservation() {
	s.mu.Lock()
	s.res.Clear()
	s.mu.Unlock()
}

func (s *callSession) reservedLiveLocked() bool {
	return s.res.ReservedLive(s.now(), callSessionReservationTTL)
}

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

	if cleared {
		if cb := s.onPresenceChange; cb != nil {
			cb()
		}
	}
}

var _ callsession_domain.CallSession = (*callSession)(nil)
