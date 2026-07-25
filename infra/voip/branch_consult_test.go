package voipinfra

import (
	"context"
	"testing"
	"time"

	"vozko/infra/voip/audio"
)

// fakeConsultPeer is the PCM side of the consult (the agent's browser end). sent
// captures audio decoded FROM the phone; recv feeds audio TOWARD the phone.
type fakeConsultPeer struct {
	sent chan []byte
	recv chan []byte
}

func newFakeConsultPeer() *fakeConsultPeer {
	return &fakeConsultPeer{sent: make(chan []byte, 64), recv: make(chan []byte, 64)}
}

func (p *fakeConsultPeer) Send(pcm16 []byte) {
	cp := append([]byte(nil), pcm16...)
	select {
	case p.sent <- cp:
	default:
	}
}
func (p *fakeConsultPeer) Recv() <-chan []byte { return p.recv }

// TestBranchConsultRelay_PhoneToPeerDecodes: G.711 from the phone is decoded to
// PCM16 and handed to the agent's consult endpoint.
func TestBranchConsultRelay_PhoneToPeerDecodes(t *testing.T) {
	phone := newFakeMediaSession()
	peer := newFakeConsultPeer()
	relay := newBranchConsultRelay(phone, peer, nil, nil)
	relay.start(context.Background())
	defer relay.stop()

	// A-law payload (phone leg negotiates PCMA in Brazil). Decode yields 2 bytes
	// (one PCM16 sample) per input byte.
	payload := audio.CodecAlaw.Encode([]byte{0x10, 0x20, 0x30, 0x40}) // 4 samples -> 4 A-law bytes
	phone.inbound <- pcmaPacket(1, payload)

	select {
	case pcm := <-peer.sent:
		if len(pcm) != len(payload)*2 {
			t.Fatalf("decoded PCM16 = %d bytes, want %d (2 per A-law byte)", len(pcm), len(payload)*2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("phone audio never reached the consult peer")
	}
}

// TestBranchConsultRelay_PeerToPhoneEncodes: the agent's PCM is re-originated as
// paced G.711 RTP toward the phone.
func TestBranchConsultRelay_PeerToPhoneEncodes(t *testing.T) {
	phone := newFakeMediaSession()
	peer := newFakeConsultPeer()
	relay := newBranchConsultRelay(phone, peer, nil, nil)
	relay.start(context.Background())
	defer relay.stop()

	// 20ms of PCM16 silence at 8kHz = 160 samples = 320 bytes.
	peer.recv <- make([]byte, 320)

	select {
	case pkt := <-phone.written:
		if len(pkt.Payload) != audio.DefaultFrameBytes {
			t.Fatalf("phone RTP payload = %d bytes, want a %d-byte G.711 frame", len(pkt.Payload), audio.DefaultFrameBytes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent audio never reached the phone")
	}
}

// TestBranchConsultRelay_StopJoinsAndKeepsPhone: stop() ends the relay
// synchronously and does NOT close the phone media (completion reuses the leg).
func TestBranchConsultRelay_StopJoins(t *testing.T) {
	phone := newFakeMediaSession()
	peer := newFakeConsultPeer()
	relay := newBranchConsultRelay(phone, peer, nil, nil)
	relay.start(context.Background())

	done := make(chan struct{})
	go func() { relay.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop() did not return (goroutines not joined)")
	}
	// Idempotent.
	relay.stop()
}

// TestBranchConsultRelay_PhoneGoneFires: a phone read error (hangup) fires
// onPhoneGone exactly once, so the transfer aborts as a target-disconnect.
func TestBranchConsultRelay_PhoneGoneFires(t *testing.T) {
	phone := newFakeMediaSession()
	peer := newFakeConsultPeer()
	var gone int
	fired := make(chan struct{}, 1)
	relay := newBranchConsultRelay(phone, peer, func() { gone++; fired <- struct{}{} }, nil)
	relay.start(context.Background())

	// Simulate the phone hanging up: unblock the reader with an error (not a
	// deliberate stop). The relay must treat this as phone-gone.
	_ = phone.UnblockReaders()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("onPhoneGone never fired on phone hangup")
	}
	relay.stop()
	if gone != 1 {
		t.Fatalf("onPhoneGone fired %d times, want exactly 1", gone)
	}
}

// TestBranchConsultRelay_PhoneGoneCallbackMayStop reproduces the real abort
// chain: onPhoneGone ends in DetachConsult -> relay.stop(). Because that callback
// used to run inline on the relay's own goroutine, stop()'s wg.Wait() self-joined
// and deadlocked, leaving the branch reserved (member stuck "on call"). The
// callback must be dispatched async so this completes.
func TestBranchConsultRelay_PhoneGoneCallbackMayStop(t *testing.T) {
	phone := newFakeMediaSession()
	peer := newFakeConsultPeer()
	done := make(chan struct{})
	var relay *branchConsultRelay
	relay = newBranchConsultRelay(phone, peer, func() {
		relay.stop() // the abort's DetachConsult path
		close(done)
	}, nil)
	relay.start(context.Background())

	_ = phone.UnblockReaders() // the phone hangs up mid-consult

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("phone-gone callback that calls stop() deadlocked (self-join)")
	}
}
