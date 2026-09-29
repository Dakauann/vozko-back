package voipinfra

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"

	"vozko/domain/voip"
)

type scriptedMedia struct {
	reads   chan any
	mu      sync.Mutex
	written []*rtp.Packet
	closed  bool
}

func newScriptedMedia() *scriptedMedia {
	return &scriptedMedia{reads: make(chan any, 32)}
}

func (m *scriptedMedia) ReadRTP(buf []byte, packet any) (int, error) {
	next, ok := <-m.reads
	if !ok {
		return 0, net.ErrClosed
	}
	if err, isErr := next.(error); isErr {
		return 0, err
	}
	*(packet.(*rtp.Packet)) = *(next.(*rtp.Packet))
	return len(buf), nil
}

func (m *scriptedMedia) WriteRTP(packet any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := packet.(*rtp.Packet)
	copied := *p
	copied.Payload = append([]byte(nil), p.Payload...)
	m.written = append(m.written, &copied)
	return nil
}

func (m *scriptedMedia) LocalAddr() net.Addr  { return &net.UDPAddr{} }
func (m *scriptedMedia) RemoteAddr() net.Addr { return &net.UDPAddr{} }
func (m *scriptedMedia) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.reads)
	}
	return nil
}

func (m *scriptedMedia) packets() []*rtp.Packet {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*rtp.Packet(nil), m.written...)
}

var pcmuCodec = voip.CodecInfo{Name: "PCMU", PayloadType: 0, SampleRate: 8000, Channels: 1, PtimeMs: 20}
var pcmaCodec = voip.CodecInfo{Name: "PCMA", PayloadType: 8, SampleRate: 8000, Channels: 1, PtimeMs: 20}

func tone(samples int) []byte {
	pcm := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16((i % 40) * 500)
		pcm[2*i] = byte(v)
		pcm[2*i+1] = byte(v >> 8)
	}
	return pcm
}

func TestPCMStreamRejectsCodecsTheBrowserCannotCarry(t *testing.T) {
	for _, codec := range []voip.CodecInfo{{Name: "opus", PayloadType: 96, SampleRate: 48000}, {}} {
		if _, err := newPCMStream(newScriptedMedia(), codec); !errors.Is(err, voip.ErrCodecNotBridgeable) {
			t.Errorf("newPCMStream(%s) error = %v, want ErrCodecNotBridgeable", codec.Name, err)
		}
	}
}

func TestPCMStreamDecodesIncomingG711Frames(t *testing.T) {
	for _, codec := range []voip.CodecInfo{pcmuCodec, pcmaCodec} {
		media := newScriptedMedia()
		stream, err := newPCMStream(media, codec)
		if err != nil {
			t.Fatal(err)
		}
		pcm := tone(160)
		encoded := g711.EncodeUlaw(pcm)
		want := g711.DecodeUlaw(encoded)
		if codec.Name == "PCMA" {
			encoded = g711.EncodeAlaw(pcm)
			want = g711.DecodeAlaw(encoded)
		}
		media.reads <- &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: codec.PayloadType}, Payload: encoded}
		select {
		case frame := <-stream.Frames():
			if string(frame) != string(want) {
				t.Fatalf("%s frame did not decode to the sent audio", codec.Name)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s: no frame decoded", codec.Name)
		}
		_ = stream.Close()
	}
}

func TestPCMStreamIgnoresPacketsOfAnotherPayloadType(t *testing.T) {
	media := newScriptedMedia()
	stream, _ := newPCMStream(media, pcmuCodec)
	defer stream.Close()
	media.reads <- &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 13}, Payload: []byte{1}}
	media.reads <- &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 0}, Payload: g711.EncodeUlaw(tone(160))}
	select {
	case frame := <-stream.Frames():
		if len(frame) != 320 {
			t.Fatalf("frame length = %d, want the voice frame", len(frame))
		}
	case <-time.After(time.Second):
		t.Fatal("voice frame never arrived")
	}
}

func TestPCMStreamPacketisesOutgoingAudioIntoTwentyMillisecondFrames(t *testing.T) {
	media := newScriptedMedia()
	stream, _ := newPCMStream(media, pcmaCodec)
	defer stream.Close()

	if err := stream.WritePCM(tone(100)); err != nil {
		t.Fatal(err)
	}
	if len(media.packets()) != 0 {
		t.Fatal("a partial frame was sent")
	}
	if err := stream.WritePCM(tone(380)); err != nil {
		t.Fatal(err)
	}
	packets := media.packets()
	if len(packets) != 3 {
		t.Fatalf("sent %d packets for 480 samples, want 3 frames of 160", len(packets))
	}
	for i, p := range packets {
		if p.PayloadType != 8 || len(p.Payload) != 160 {
			t.Fatalf("packet %d = PT %d len %d, want PCMA 160 bytes", i, p.PayloadType, len(p.Payload))
		}
		if i > 0 {
			if p.SequenceNumber != packets[i-1].SequenceNumber+1 || p.Timestamp != packets[i-1].Timestamp+160 || p.SSRC != packets[0].SSRC {
				t.Fatalf("packet %d breaks the RTP sequence: %+v after %+v", i, p.Header, packets[i-1].Header)
			}
		}
	}
	if !packets[0].Marker {
		t.Fatal("the first packet must carry the marker bit")
	}
}

func TestPCMStreamClosesFramesWhenTheMediaEnds(t *testing.T) {
	media := newScriptedMedia()
	stream, _ := newPCMStream(media, pcmuCodec)
	media.reads <- os.ErrDeadlineExceeded
	media.reads <- io.ErrUnexpectedEOF
	select {
	case _, open := <-stream.Frames():
		if open {
			t.Fatal("received a frame from a failed media session")
		}
	case <-time.After(time.Second):
		t.Fatal("frames never closed after the media failed")
	}
	select {
	case <-stream.Done():
	default:
		t.Fatal("Done() not closed after the media ended")
	}
}

func TestPCMStreamCloseClosesTheMediaOnceAndStopsWrites(t *testing.T) {
	media := newScriptedMedia()
	stream, _ := newPCMStream(media, pcmuCodec)
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	if err := stream.WritePCM(tone(160)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("WritePCM after Close = %v, want net.ErrClosed", err)
	}
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() not closed after Close()")
	}
}
