package ws

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"vozko/domain/callrouting"
	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/voip"
	calls_usecase "vozko/usecases/calls"
	callsession_usecase "vozko/usecases/callsession"
)

var errForeignSession = errors.New("call can only connect to a call session of this server")

type CallChannels struct {
	mu        sync.RWMutex
	calls     map[string]*liveCall
	activity  callrouting.AgentActivity
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner
	endCall   callsession_domain.EndOutboundCallUseCase
	recording *calls_usecase.RecordingUploadPool
	logger    *log.Logger
}

var (
	_ callrouting.CallDirectory = (*CallChannels)(nil)
	_ callrouting.CallParking   = (*CallChannels)(nil)
)

func NewCallChannels(
	activity callrouting.AgentActivity,
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner,
	endCall callsession_domain.EndOutboundCallUseCase,
	recording *calls_usecase.RecordingUploadPool,
	logger *log.Logger,
) *CallChannels {
	if logger == nil {
		logger = log.Default()
	}
	return &CallChannels{
		calls:     map[string]*liveCall{},
		activity:  activity,
		lifecycle: lifecycle,
		endCall:   endCall,
		recording: recording,
		logger:    logger,
	}
}

func channelKey(workspaceID, callID string) string {
	return workspaceID + "|" + callID
}

func (c *CallChannels) track(lc *liveCall) func() {
	if c == nil || lc == nil || lc.call == nil {
		return func() {}
	}
	key := channelKey(lc.workspaceID, lc.call.ID())
	c.mu.Lock()
	c.calls[key] = lc
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		if c.calls[key] == lc {
			delete(c.calls, key)
		}
		c.mu.Unlock()
	}
}

func (c *CallChannels) Find(workspaceID, callID string) (callrouting.RoutedCall, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	lc, ok := c.calls[channelKey(workspaceID, callID)]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return lc, true
}

func (c *CallChannels) Park(ctx context.Context, input callrouting.ParkInput) (callrouting.RoutedCall, error) {
	if input.Call == nil {
		return nil, errors.New("park: call is required")
	}
	if c.lifecycle == nil {
		return nil, callsession_domain.ErrBillingNotConfigured
	}
	lc := buildLiveCall(callAttachInput{
		Call:          input.Call,
		Admission:     input.Admission,
		Phone:         input.Phone,
		WorkspaceID:   input.WorkspaceID,
		StartedAt:     input.StartedAt,
		Direction:     input.Direction,
		RecordingPool: c.recording,
	})
	untrack := c.track(lc)
	lc.start(ctx, c.lifecycle, c.endCall, c.logger, untrack)
	return lc, nil
}

type holdPlayback struct {
	stop chan struct{}
	once sync.Once
	done chan struct{}
}

func (h *holdPlayback) end() {
	h.once.Do(func() { close(h.stop) })
	<-h.done
}

func (lc *liveCall) ID() string { return lc.call.ID() }

func (lc *liveCall) WorkspaceID() string { return lc.workspaceID }

func (lc *liveCall) RemoteNumber() string { return lc.phone }

func (lc *liveCall) Channel() string { return callsession_domain.OfferChannelFor(lc.call.ID()) }

func (lc *liveCall) OwnerSession() (callsession_domain.CallSession, bool) {
	if s := lc.forwarder.Load(); s != nil {
		return s, true
	}
	return nil, false
}

func (lc *liveCall) SendAudio(pcm []byte) error {
	if !lc.enqueueAudio(pcm) {
		return errors.New("call audio uplink is not running")
	}
	return nil
}

func (lc *liveCall) Hangup() error { return lc.call.Hangup() }

func (lc *liveCall) Hold(music []byte) error {
	if len(music) == 0 {
		return voip.ErrNothingToPlay
	}
	if owner := lc.forwarder.Load(); owner != nil {
		owner.release(lc)
	}
	playback := &holdPlayback{stop: make(chan struct{}), done: make(chan struct{})}
	lc.swapHold(playback)
	go func() {
		defer close(playback.done)
		stop := make(chan struct{})
		go func() {
			select {
			case <-playback.stop:
			case <-lc.lifecycleDone:
			}
			close(stop)
		}()
		_ = voip.LoopPCM(music, func(frame []byte) error {
			lc.enqueueAudio(frame)
			return nil
		}, stop)
	}()
	return nil
}

func (lc *liveCall) StopHold() { lc.swapHold(nil) }

func (lc *liveCall) Connect(session callsession_domain.CallSession) error {
	target, ok := session.(*callSession)
	if !ok || target == nil {
		return errForeignSession
	}
	lc.swapHold(nil)
	if err := target.Attach(lc); err != nil {
		return err
	}
	lc.answered.Store(true)
	target.dispatchStatus(lc, conversation.CallEvent{Type: conversation.CallEventAnswered})
	return nil
}

func (lc *liveCall) swapHold(next *holdPlayback) {
	lc.holdMu.Lock()
	previous := lc.hold
	lc.hold = next
	lc.holdMu.Unlock()
	if previous != nil {
		previous.end()
	}
}

var _ callrouting.RoutedCall = (*liveCall)(nil)

func recordActivity(activity callrouting.AgentActivity, workspaceID, userID string, event conversation.CallEvent) {
	if activity == nil || userID == "" {
		return
	}
	switch {
	case event.Type == conversation.CallEventAnswered:
		activity.CallAnswered(workspaceID, userID)
	case event.IsTerminal():
		activity.CallEnded(workspaceID, userID, time.Now())
	}
}
