package voipinfra

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
	"vozko/infra/voip/voiptest"
)

type progressLog struct {
	mu     sync.Mutex
	alerts int
	early  voip.PCMStream
	ready  chan voip.PCMStream
}

func newProgressLog() *progressLog { return &progressLog{ready: make(chan voip.PCMStream, 1)} }

func (p *progressLog) Alerting() {
	p.mu.Lock()
	p.alerts++
	p.mu.Unlock()
}

func (p *progressLog) EarlyMedia(audio voip.PCMStream) {
	p.mu.Lock()
	p.early = audio
	p.mu.Unlock()
	p.ready <- audio
}

func (p *progressLog) alerted() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.alerts
}

type inviteResult struct {
	session sip_trunk.TrunkCallSession
	err     error
}

func inviteInBackground(f engineFixture, progress sip_trunk.InviteProgress) <-chan inviteResult {
	done := make(chan inviteResult, 1)
	go func() {
		session, err := f.manager.Invite(context.Background(), f.trunk.ID, sip_trunk.TrunkInviteInput{PhoneNumber: "100", Progress: progress})
		done <- inviteResult{session, err}
	}()
	return done
}

func TestEngineReportsTheFarEndRingingBeforeTheAnswer(t *testing.T) {
	f := startEngine(t, nil)
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		time.Sleep(200 * time.Millisecond)
		voiptest.AnswerAndHold(nil)(d)
	})
	progress := newProgressLog()

	result := <-inviteInBackground(f, progress)
	if result.err != nil {
		t.Fatalf("Invite() error = %v", result.err)
	}
	if progress.alerted() == 0 {
		t.Fatal("180 Ringing was not reported as alerting")
	}
	if progress.early != nil {
		t.Fatal("a plain ringing call reported early media")
	}
	_ = f.manager.Hangup(context.Background(), f.trunk.ID, result.session.ID)
	f.waitNoCalls(t)
}

func TestEngineHandsTheCarriersEarlyMediaOverWithoutStartingTheCallClock(t *testing.T) {
	f := startEngine(t, nil)
	answer := make(chan struct{})
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		if err := d.ProgressMedia(); err != nil {
			return
		}
		voiptest.SendRTP(d, 10)
		<-answer
		voiptest.AnswerAndHold(nil)(d)
	})
	progress := newProgressLog()
	result := inviteInBackground(f, progress)

	var early voip.PCMStream
	select {
	case early = <-progress.ready:
	case <-time.After(eventually):
		t.Fatal("183 Session Progress media was not handed over")
	}
	if frame := readFrame(t, early); len(frame) != voip.PCMFrameBytes {
		t.Fatalf("early frame = %d bytes", len(frame))
	}
	if calls := f.manager.ActiveCalls(f.trunk.ID); len(calls) != 0 {
		t.Fatalf("ActiveCalls() = %+v during early media, want the call unanswered", calls)
	}

	close(answer)
	answered := <-result
	if answered.err != nil {
		t.Fatalf("Invite() error = %v", answered.err)
	}
	if answered.session.Audio != early {
		t.Fatal("the answered call must keep the early media stream")
	}
	if answered.session.AnsweredAt.IsZero() {
		t.Fatal("the answer was not stamped")
	}
	_ = f.manager.Hangup(context.Background(), f.trunk.ID, answered.session.ID)
	f.waitNoCalls(t)
}

func TestEngineReportsARejectionAfterRinging(t *testing.T) {
	f := startEngine(t, nil)
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		time.Sleep(100 * time.Millisecond)
		_ = d.Respond(sip.StatusBusyHere, "Busy Here", nil)
	})
	progress := newProgressLog()

	result := <-inviteInBackground(f, progress)
	var rejected *sip_trunk.CallRejectedError
	if !errors.As(result.err, &rejected) || rejected.StatusCode != sip.StatusBusyHere {
		t.Fatalf("Invite() error = %v, want 486", result.err)
	}
	if progress.alerted() == 0 {
		t.Fatal("the ringing before the rejection was not reported")
	}
	f.waitNoCalls(t)
}

func TestEngineClosesEarlyMediaWhenTheCallIsRejected(t *testing.T) {
	f := startEngine(t, nil)
	f.provider.OnCall(func(d *diago.DialogServerSession) {
		if err := d.ProgressMedia(); err != nil {
			return
		}
		voiptest.SendRTP(d, 5)
		_ = d.Respond(sip.StatusServiceUnavailable, "Service Unavailable", nil)
	})
	progress := newProgressLog()
	result := inviteInBackground(f, progress)

	early := <-progress.ready
	if res := <-result; res.err == nil {
		t.Fatal("a rejected call was reported as answered")
	}
	select {
	case <-early.Done():
	case <-time.After(eventually):
		t.Fatal("the early media stream leaked after the rejection")
	}
	f.waitNoCalls(t)
}
