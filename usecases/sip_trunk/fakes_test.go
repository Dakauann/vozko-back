package sip_trunk_usecase

import (
	"context"
	"net"
	"sync"
	"time"

	"vozko/domain/sip_trunk"
)

type fakePCM struct {
	frames    chan []byte
	done      chan struct{}
	mu        sync.Mutex
	written   [][]byte
	closeOnce sync.Once
}

func newFakePCM() *fakePCM {
	return &fakePCM{frames: make(chan []byte, 8), done: make(chan struct{})}
}

func (p *fakePCM) Frames() <-chan []byte { return p.frames }
func (p *fakePCM) Done() <-chan struct{} { return p.done }

func (p *fakePCM) WritePCM(pcm []byte) error {
	select {
	case <-p.done:
		return net.ErrClosed
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.written = append(p.written, append([]byte(nil), pcm...))
	return nil
}

func (p *fakePCM) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
		close(p.frames)
	})
	return nil
}

func (p *fakePCM) writes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.written)
}

type fakeEngine struct {
	mu          sync.Mutex
	registered  []string
	refreshed   []*sip_trunk.SIPTrunk
	unregisterd []string
	invites     []sip_trunk.TrunkInviteInput
	hangups     []string
	statuses    map[string]sip_trunk.SIPTrunkStatusUpdate
	calls       map[string][]sip_trunk.ActiveCall
	sessions    map[string]*fakePCM
	inviteErr   error
	inviteWait  chan struct{}
	alerting    bool
	earlyMedia  bool
	registerErr error
	hangupErr   error
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		statuses: map[string]sip_trunk.SIPTrunkStatusUpdate{},
		calls:    map[string][]sip_trunk.ActiveCall{},
		sessions: map[string]*fakePCM{},
	}
}

func (e *fakeEngine) markRegistered(trunkID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.statuses[trunkID] = sip_trunk.SIPTrunkStatusUpdate{TrunkID: trunkID, Status: sip_trunk.RegistrationStatusRegistered}
}

func (e *fakeEngine) RegisterTrunk(trunk *sip_trunk.SIPTrunk) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.registerErr != nil {
		return e.registerErr
	}
	e.registered = append(e.registered, trunk.ID)
	e.statuses[trunk.ID] = sip_trunk.SIPTrunkStatusUpdate{TrunkID: trunk.ID, Status: sip_trunk.RegistrationStatusRegistering}
	return nil
}

func (e *fakeEngine) RefreshTrunk(trunk *sip_trunk.SIPTrunk) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	copied := *trunk
	e.refreshed = append(e.refreshed, &copied)
	return nil
}

func (e *fakeEngine) UnregisterTrunk(trunkID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unregisterd = append(e.unregisterd, trunkID)
	return nil
}

func (e *fakeEngine) TrunkStatus(trunkID string) (sip_trunk.SIPTrunkStatusUpdate, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	status, ok := e.statuses[trunkID]
	return status, ok
}

func (e *fakeEngine) Invite(ctx context.Context, trunkID string, input sip_trunk.TrunkInviteInput) (sip_trunk.TrunkCallSession, error) {
	audio := newFakePCM()
	e.mu.Lock()
	e.invites = append(e.invites, input)
	e.sessions["engine-call-1"] = audio
	wait, inviteErr, alerting, earlyMedia := e.inviteWait, e.inviteErr, e.alerting, e.earlyMedia
	e.mu.Unlock()
	if alerting {
		input.Progress.Alerting()
	}
	if earlyMedia {
		input.Progress.EarlyMedia(audio)
	}
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return sip_trunk.TrunkCallSession{}, ctx.Err()
		}
	}
	if inviteErr != nil {
		_ = audio.Close()
		return sip_trunk.TrunkCallSession{}, inviteErr
	}
	return sip_trunk.TrunkCallSession{
		ID:          "engine-call-1",
		TrunkID:     trunkID,
		PhoneNumber: input.PhoneNumber,
		Direction:   sip_trunk.CallDirectionOutbound,
		StartedAt:   time.Now(),
		AnsweredAt:  time.Now(),
		Audio:       audio,
	}, nil
}

func (e *fakeEngine) session(id string) *fakePCM {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessions[id]
}

func (e *fakeEngine) Hangup(_ context.Context, trunkID, callID string) error {
	e.mu.Lock()
	e.hangups = append(e.hangups, trunkID+"/"+callID)
	audio := e.sessions[callID]
	err := e.hangupErr
	e.mu.Unlock()
	if audio != nil {
		_ = audio.Close()
	}
	return err
}

func (e *fakeEngine) hangupCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.hangups)
}

func (e *fakeEngine) inviteCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.invites)
}

func (e *fakeEngine) ActiveCalls(trunkID string) []sip_trunk.ActiveCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls[trunkID]
}
