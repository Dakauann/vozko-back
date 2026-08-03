package dialer

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"

	"vozko/domain/voip"
)

type nopMediaSession struct {
	closed    atomic.Bool
	stop      chan struct{}
	closeErr  error
	codecName string // negotiated codec name; "" => µ-law
}

func newNopMediaSession() *nopMediaSession {
	return &nopMediaSession{stop: make(chan struct{})}
}

func (m *nopMediaSession) ReadRTP(_ []byte, _ interface{}) (int, error) {
	<-m.stop
	return 0, errors.New("closed")
}

func (m *nopMediaSession) WriteRTP(_ interface{}) error { return nil }

func (m *nopMediaSession) Close() error {
	if m.closed.CompareAndSwap(false, true) {
		close(m.stop)
	}
	return m.closeErr
}

func (m *nopMediaSession) LocalAddr() net.Addr       { return nil }
func (m *nopMediaSession) RemoteAddr() net.Addr      { return nil }
func (m *nopMediaSession) OnDTMF(_ voip.DTMFHandler) {}
func (m *nopMediaSession) UnblockReaders() error     { return nil }
func (m *nopMediaSession) NegotiatedCodec() voip.CodecInfo {
	return voip.CodecInfo{Name: m.codecName}
}

func (m *nopMediaSession) wasClosed() bool { return m.closed.Load() }

func TestPassthroughHangup_InvokesTrunkHangup(t *testing.T) {
	t.Parallel()

	media := newNopMediaSession()
	var hangupCalls atomic.Int32
	trunkHangup := func() error {
		hangupCalls.Add(1)
		return nil
	}

	pc := NewVoipPassthroughCall("call-1", "+5511999999999", media, trunkHangup, nil)
	if err := pc.Hangup(); err != nil {
		t.Fatalf("Hangup returned unexpected error: %v", err)
	}
	if got := hangupCalls.Load(); got != 1 {
		t.Fatalf("expected trunk hangup closure invoked exactly once, got %d", got)
	}
	if !media.wasClosed() {
		t.Fatal("expected media.Close to be invoked")
	}
}

func TestPassthroughHangup_IsIdempotent(t *testing.T) {
	t.Parallel()

	media := newNopMediaSession()
	var hangupCalls atomic.Int32
	trunkHangup := func() error {
		hangupCalls.Add(1)
		return nil
	}

	pc := NewVoipPassthroughCall("call-1", "+5511999999999", media, trunkHangup, nil)
	_ = pc.Hangup()
	_ = pc.Hangup()
	_ = pc.Hangup()

	if got := hangupCalls.Load(); got != 1 {
		t.Fatalf("expected trunk hangup closure invoked exactly once across repeated Hangups, got %d", got)
	}
}

func TestPassthroughHangup_NilTrunkHangup_NoCrash(t *testing.T) {
	t.Parallel()

	media := newNopMediaSession()
	pc := NewVoipPassthroughCall("call-1", "+5511999999999", media, nil, nil)
	if err := pc.Hangup(); err != nil {
		t.Fatalf("Hangup with nil trunkHangup returned error: %v", err)
	}
	if !media.wasClosed() {
		t.Fatal("expected media.Close to be invoked even without trunk hangup")
	}
}

func TestPassthroughHangup_TrunkHangupError_StillClosesMedia(t *testing.T) {
	t.Parallel()

	media := newNopMediaSession()
	trunkHangup := func() error { return errors.New("trunk unreachable") }

	pc := NewVoipPassthroughCall("call-1", "+5511999999999", media, trunkHangup, nil)
	if err := pc.Hangup(); err == nil {
		t.Fatal("expected Hangup to surface trunk hangup error")
	}
	if !media.wasClosed() {
		t.Fatal("media.Close MUST still be invoked even when trunk hangup fails")
	}
}

// recordingMediaSession captures every RTP header written, for asserting on the
// transmitted uplink stream (decimation behaviour).
type recordingMediaSession struct {
	*nopMediaSession
	mu      sync.Mutex
	packets []rtp.Header
}

func newRecordingMediaSession() *recordingMediaSession {
	return &recordingMediaSession{nopMediaSession: newNopMediaSession()}
}

func (m *recordingMediaSession) WriteRTP(p interface{}) error {
	if pkt, ok := p.(*rtp.Packet); ok {
		m.mu.Lock()
		m.packets = append(m.packets, pkt.Header)
		m.mu.Unlock()
	}
	return nil
}

func (m *recordingMediaSession) headers() []rtp.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]rtp.Header(nil), m.packets...)
}

// The pacer must let the buffered lead (one-way latency) be measured and pulled
// back, the primitives SendAudio uses to bound uplink delay.
func TestPassthroughPacer_DropFrameReducesLead(t *testing.T) {
	t.Parallel()

	p := newPassthroughPacer(passthroughSampleRate)
	// Start it (first Wait returns immediately and anchors the schedule).
	if err := p.Wait(context.Background(), passthroughFrameSamples); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	// Force a large lead, as if the producer outpaced real time for a while.
	p.nextSend = time.Now().Add(500 * time.Millisecond)
	lead := p.lead()
	if lead < 400*time.Millisecond {
		t.Fatalf("expected ~500ms lead, got %v", lead)
	}
	p.dropFrame(passthroughFrameSamples)
	after := p.lead()
	if after >= lead {
		t.Fatalf("dropFrame should reduce the lead: before=%v after=%v", lead, after)
	}
	if diff := lead - after; diff < 15*time.Millisecond || diff > 25*time.Millisecond {
		t.Fatalf("dropFrame should pull the lead back by ~one frame (20ms), got %v", diff)
	}
}

// Regression test for the growing browser→SIP delay: when the pacer has drifted
// far ahead of real time, SendAudio must DECIMATE (drop frames) to bound the
// one-way latency, and transmitted frames must keep contiguous RTP timestamps/seq
// (decimation removes audio; it must not punch gaps into the timeline).
func TestPassthroughSendAudio_BoundsUplinkLatencyByDecimating(t *testing.T) {
	t.Parallel()

	media := newRecordingMediaSession()
	pc := NewVoipPassthroughCall("call-decimate", "+5511999999999", media, nil, log.New(io.Discard, "", 0))

	// One frame to lazily build + anchor the pacer.
	if err := pc.SendAudio(make([]byte, passthroughFrameSamples*2)); err != nil {
		t.Fatalf("init SendAudio: %v", err)
	}

	// Simulate the browser audio clock having outpaced our send clock: a big lead.
	pc.writeMu.Lock()
	pc.writePacer.nextSend = time.Now().Add(300 * time.Millisecond)
	pc.writeMu.Unlock()

	// Feed a backlog (20 frames). Decimation should drop the excess instead of
	// buffering it as ever-growing latency.
	const frames = 20
	if err := pc.SendAudio(make([]byte, passthroughFrameSamples*2*frames)); err != nil {
		t.Fatalf("backlog SendAudio: %v", err)
	}

	pc.writeMu.Lock()
	dropped := pc.droppedFrames
	lead := pc.writePacer.lead()
	pc.writeMu.Unlock()

	if dropped == 0 {
		t.Fatal("expected decimation to drop frames once the pacer lead exceeded budget")
	}
	if lead > passthroughMaxUplinkLead+25*time.Millisecond {
		t.Fatalf("uplink latency not bounded: pacer lead %v exceeds budget %v", lead, passthroughMaxUplinkLead)
	}

	hs := media.headers()
	if len(hs) < 2 {
		t.Fatalf("expected several transmitted frames after catch-up, got %d", len(hs))
	}
	for i := 1; i < len(hs); i++ {
		if hs[i].Timestamp != hs[i-1].Timestamp+passthroughFrameSamples {
			t.Fatalf("non-contiguous RTP timestamp at frame %d: %d then %d (decimation must not punch gaps)",
				i, hs[i-1].Timestamp, hs[i].Timestamp)
		}
		if hs[i].SequenceNumber != hs[i-1].SequenceNumber+1 {
			t.Fatalf("non-contiguous RTP seq at frame %d: %d then %d",
				i, hs[i-1].SequenceNumber, hs[i].SequenceNumber)
		}
	}
}
