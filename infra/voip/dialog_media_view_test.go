package voipinfra

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emiago/diago/media"
	"github.com/pion/rtp"
)

type mediaFixture struct {
	session *media.MediaSession
	peer    *net.UDPConn
	local   net.Addr
}

func newMediaFixture(t *testing.T, codecs ...media.Codec) mediaFixture {
	t.Helper()
	rtpConn := listenLocalUDP(t)
	rtcpConn := listenLocalUDP(t)
	peer := listenLocalUDP(t)
	session := &media.MediaSession{Codecs: codecs}
	session.InitWithListeners(rtpConn, rtcpConn, peer.LocalAddr().(*net.UDPAddr))
	return mediaFixture{session: session, peer: peer, local: rtpConn.LocalAddr()}
}

func (f mediaFixture) send(t *testing.T, p *rtp.Packet) {
	t.Helper()
	raw, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.peer.WriteTo(raw, f.local); err != nil {
		t.Fatal(err)
	}
}

func dtmfEvent(seq uint16, timestamp uint32, digit byte, end bool) *rtp.Packet {
	flags := byte(10)
	if end {
		flags |= 0x80
	}
	return &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 101, SequenceNumber: seq, Timestamp: timestamp},
		Payload: []byte{digit, flags, 0, 160},
	}
}

func TestDialogMediaViewFiltersDTMFAndFiresEachDigitOnce(t *testing.T) {
	fixture := newMediaFixture(t, media.CodecAudioUlaw, media.CodecTelephoneEvent8000)
	adapter := newDialogMediaView(func() *media.MediaSession { return fixture.session })
	var mu sync.Mutex
	var digits []rune
	adapter.OnDTMF(func(d rune) {
		mu.Lock()
		digits = append(digits, d)
		mu.Unlock()
	})

	fixture.send(t, dtmfEvent(1, 1000, 5, false))
	fixture.send(t, dtmfEvent(2, 1000, 5, true))
	fixture.send(t, dtmfEvent(3, 1000, 5, true))
	fixture.send(t, dtmfEvent(4, 1000, 5, true))
	fixture.send(t, dtmfEvent(5, 2000, 11, true))
	fixture.send(t, &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 0, SequenceNumber: 6}, Payload: []byte{1, 2, 3}})

	done := make(chan rtp.Packet, 1)
	go func() {
		var p rtp.Packet
		if _, err := adapter.ReadRTP(make([]byte, 1500), &p); err == nil {
			done <- p
		}
	}()
	select {
	case p := <-done:
		if p.PayloadType != 0 || p.SequenceNumber != 6 {
			t.Fatalf("read PT %d seq %d, want the voice packet", p.PayloadType, p.SequenceNumber)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadRTP() never returned the voice packet")
	}
	mu.Lock()
	defer mu.Unlock()
	if string(digits) != "5#" {
		t.Fatalf("DTMF digits = %q, want \"5#\": retransmitted end packets must fire once", string(digits))
	}
}

func TestDialogMediaViewFollowsTheCurrentSessionAfterAMediaUpdate(t *testing.T) {
	first := newMediaFixture(t, media.CodecAudioUlaw)
	second := newMediaFixture(t, media.CodecAudioAlaw)
	current := first.session
	adapter := newDialogMediaView(func() *media.MediaSession { return current })
	if got := adapter.NegotiatedCodec().Name; got != "PCMU" {
		t.Fatalf("NegotiatedCodec() = %q, want PCMU", got)
	}
	current = second.session
	if got := adapter.NegotiatedCodec().Name; got != "PCMA" {
		t.Fatalf("NegotiatedCodec() after update = %q, want PCMA", got)
	}
	if adapter.RemoteAddr().String() != second.peer.LocalAddr().String() {
		t.Fatalf("RemoteAddr() = %v, want the updated peer %v", adapter.RemoteAddr(), second.peer.LocalAddr())
	}
}

func TestNegotiatedVoiceCodecSkipsTelephoneEvent(t *testing.T) {
	session := &media.MediaSession{Codecs: []media.Codec{media.CodecTelephoneEvent8000, media.CodecAudioAlaw}}
	codec, ok := negotiatedVoiceCodec(session)
	if !ok || codec.Name != "PCMA" {
		t.Fatalf("negotiatedVoiceCodec() = %v, %v, want PCMA", codec, ok)
	}
	info := buildMediaInfo(session)
	if info.Codec.Name != "PCMA" || info.Codec.PtimeMs != 20 || info.Codec.SampleRate != 8000 || info.DTMFPayloadType != 101 || info.Encrypted {
		t.Fatalf("buildMediaInfo() = %+v", info)
	}
}

func TestDialogMediaViewLeavesTheSocketToTheDialog(t *testing.T) {
	fixture := newMediaFixture(t, media.CodecAudioUlaw)
	view := newDialogMediaView(func() *media.MediaSession { return fixture.session })
	if err := view.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := fixture.session.Close(); err != nil {
		t.Fatalf("dialog-owned session close after view Close = %v, want nil: the view must not close the socket", err)
	}
}
