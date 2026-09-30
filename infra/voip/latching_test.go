package voipinfra

import (
	"net"
	"testing"
	"time"

	"github.com/emiago/diago/media"
	"github.com/pion/rtp"
)

func anySource(*net.UDPAddr) bool { return true }

func onlyFrom(trusted net.Addr) func(*net.UDPAddr) bool {
	return func(source *net.UDPAddr) bool { return source.String() == trusted.String() }
}

func TestLatchingConnNeverLatchesOntoAnUntrustedSender(t *testing.T) {
	local := listenLocalUDP(t)
	provider := listenLocalUDP(t)
	attacker := listenLocalUDP(t)
	latch := newLatchingConn(local, []uint8{0}, onlyFrom(provider.LocalAddr()))

	_, _ = attacker.WriteTo(rtpBytes(t, 0, 66), local.LocalAddr())
	_, _ = provider.WriteTo(rtpBytes(t, 0, 1), local.LocalAddr())
	raw, from, err := readWithin(t, latch)
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}
	var p rtp.Packet
	if err := p.Unmarshal(raw); err != nil || p.SequenceNumber != 1 || from.String() != provider.LocalAddr().String() {
		t.Fatalf("read seq %d from %v, want the provider's first packet: a spoofed sender must not capture the call", p.SequenceNumber, from)
	}
	if _, err := latch.WriteTo([]byte("voice"), provider.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readWithin(t, attacker); err == nil {
		t.Fatal("the attacker received the call's audio")
	}
}

func listenLocalUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func rtpBytes(t *testing.T, pt uint8, seq uint16) []byte {
	t.Helper()
	raw, err := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: seq}, Payload: []byte{1, 2}}).Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func readWithin(t *testing.T, conn net.PacketConn) ([]byte, net.Addr, error) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 1500)
	n, from, err := conn.ReadFrom(buf)
	return buf[:n], from, err
}

func TestLatchingConnLatchesOnTheFirstExpectedRTPAndSendsBackThere(t *testing.T) {
	local := listenLocalUDP(t)
	natPeer := listenLocalUDP(t)
	signaled := listenLocalUDP(t)
	latch := newLatchingConn(local, []uint8{0, 101}, anySource)

	if _, err := natPeer.WriteTo(rtpBytes(t, 0, 1), local.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readWithin(t, latch); err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}
	if _, err := latch.WriteTo([]byte("hello"), signaled.LocalAddr()); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if got, _, err := readWithin(t, natPeer); err != nil || string(got) != "hello" {
		t.Fatalf("NAT peer received %q, %v, want hello: media must follow the latched source", got, err)
	}
}

func TestLatchingConnDropsPacketsFromOtherSourcesOnceLatched(t *testing.T) {
	local := listenLocalUDP(t)
	peer := listenLocalUDP(t)
	intruder := listenLocalUDP(t)
	latch := newLatchingConn(local, []uint8{0}, anySource)

	_, _ = peer.WriteTo(rtpBytes(t, 0, 1), local.LocalAddr())
	if _, _, err := readWithin(t, latch); err != nil {
		t.Fatalf("first read error = %v", err)
	}
	_, _ = intruder.WriteTo(rtpBytes(t, 0, 99), local.LocalAddr())
	_, _ = peer.WriteTo(rtpBytes(t, 0, 2), local.LocalAddr())
	raw, from, err := readWithin(t, latch)
	if err != nil {
		t.Fatalf("second read error = %v", err)
	}
	var p rtp.Packet
	if err := p.Unmarshal(raw); err != nil || p.SequenceNumber != 2 || from.String() != peer.LocalAddr().String() {
		t.Fatalf("read seq %d from %v, want seq 2 from the latched peer: injected media must be dropped", p.SequenceNumber, from)
	}
}

func TestLatchingConnIgnoresNonRTPAndUnexpectedPayloadTypesBeforeLatching(t *testing.T) {
	local := listenLocalUDP(t)
	peer := listenLocalUDP(t)
	latch := newLatchingConn(local, []uint8{8}, anySource)

	_, _ = peer.WriteTo([]byte("not rtp"), local.LocalAddr())
	_, _ = peer.WriteTo(rtpBytes(t, 96, 1), local.LocalAddr())
	_, _ = peer.WriteTo(rtpBytes(t, 8, 2), local.LocalAddr())
	raw, _, err := readWithin(t, latch)
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}
	var p rtp.Packet
	if err := p.Unmarshal(raw); err != nil || p.SequenceNumber != 2 {
		t.Fatalf("read seq %d, want 2", p.SequenceNumber)
	}
}

func TestLatchingConnResetLetsANewSourceLatchAfterAMediaUpdate(t *testing.T) {
	local := listenLocalUDP(t)
	first := listenLocalUDP(t)
	second := listenLocalUDP(t)
	latch := newLatchingConn(local, []uint8{0}, anySource)

	_, _ = first.WriteTo(rtpBytes(t, 0, 1), local.LocalAddr())
	if _, _, err := readWithin(t, latch); err != nil {
		t.Fatal(err)
	}
	latch.Reset()
	if _, err := latch.WriteTo([]byte("to-signaled"), second.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if got, _, err := readWithin(t, second); err != nil || string(got) != "to-signaled" {
		t.Fatalf("after Reset writes must go to the newly signaled address, got %q, %v", got, err)
	}
	_, _ = second.WriteTo(rtpBytes(t, 0, 5), local.LocalAddr())
	if _, from, err := readWithin(t, latch); err != nil || from.String() != second.LocalAddr().String() {
		t.Fatalf("after Reset read from %v, %v, want the new source", from, err)
	}
}

func TestAttachLatchReplacesTheMediaSessionConnection(t *testing.T) {
	rtpConn := listenLocalUDP(t)
	rtcpConn := listenLocalUDP(t)
	session := &media.MediaSession{Codecs: []media.Codec{media.CodecAudioUlaw, media.CodecTelephoneEvent8000}}
	session.InitWithListeners(rtpConn, rtcpConn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})

	latch, err := attachLatch(session, anySource)
	if err != nil {
		t.Fatalf("attachLatch() error = %v", err)
	}
	conn, err := sessionRTPConn(session)
	if err != nil {
		t.Fatalf("sessionRTPConn() error = %v", err)
	}
	if conn != net.PacketConn(latch) {
		t.Fatal("media session still reads from the raw connection")
	}
	again, err := attachLatch(session, anySource)
	if err != nil || again != latch {
		t.Fatalf("attachLatch() twice = %v, %v, want the existing latch", again, err)
	}
}

func TestAttachLatchRejectsASessionWithoutConnection(t *testing.T) {
	if _, err := attachLatch(&media.MediaSession{}, anySource); err == nil {
		t.Fatal("attachLatch() on an uninitialised session = nil error, want error")
	}
}

func TestSessionEncryptedIsFalseForPlainRTP(t *testing.T) {
	session := &media.MediaSession{}
	encrypted, err := sessionEncrypted(session)
	if err != nil || encrypted {
		t.Fatalf("sessionEncrypted() = %v, %v, want false, nil", encrypted, err)
	}
}

func TestPayloadTypesFromCodecs(t *testing.T) {
	got := payloadTypesFromCodecs([]media.Codec{media.CodecAudioUlaw, media.CodecAudioAlaw, media.CodecTelephoneEvent8000})
	want := []uint8{0, 8, 101}
	if len(got) != len(want) {
		t.Fatalf("payloadTypesFromCodecs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("payloadTypesFromCodecs() = %v, want %v", got, want)
		}
	}
}
