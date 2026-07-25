package calls_usecase

import (
	"net"
	"sync"

	"github.com/pion/rtp"

	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

type RecordingMediaSession struct {
	inner    voip.MediaSession
	recorder *CallRecorder
	pool     *RecordingUploadPool
	mu       sync.Mutex
	closed   bool
}

func NewRecordingMediaSession(inner voip.MediaSession, callID, workspaceID, entryID, leadID string) *RecordingMediaSession {
	return &RecordingMediaSession{
		inner:    inner,
		recorder: NewCallRecorder(callID, workspaceID, entryID, leadID),
	}
}

func (s *RecordingMediaSession) SetPool(p *RecordingUploadPool) *RecordingMediaSession {
	s.pool = p
	return s
}

func (s *RecordingMediaSession) Recorder() *CallRecorder {
	return s.recorder
}

func (s *RecordingMediaSession) Finish() {
	if s.recorder != nil && s.pool != nil {
		s.recorder.Finish(s.pool)
	}
}

func (s *RecordingMediaSession) ReadRTP(buf []byte, packet interface{}) (int, error) {
	n, err := s.inner.ReadRTP(buf, packet)
	if err != nil || n == 0 {
		return n, err
	}
	if pkt, ok := packet.(*rtp.Packet); ok {
		// Decode by the packet's own payload type so A-law (PT 8) records
		// correctly, not just µ-law. Non-G.711 payloads (DTMF, etc.) are skipped.
		if len(pkt.Payload) > 0 {
			if codec, ok := audio.CodecForPayloadType(pkt.PayloadType); ok {
				s.recorder.RecordCallerPCM(codec.Decode(pkt.Payload))
			}
		}
	}
	return n, err
}

func (s *RecordingMediaSession) WriteRTP(packet interface{}) error {
	err := s.inner.WriteRTP(packet)
	if err != nil {
		return err
	}
	if pkt, ok := packet.(*rtp.Packet); ok {
		// The outbound packet carries the codec we actually send (PT 0 µ-law or
		// PT 8 A-law); decode accordingly so recordings never become noise.
		if len(pkt.Payload) > 0 {
			if codec, ok := audio.CodecForPayloadType(pkt.PayloadType); ok {
				pcm := codec.Decode(pkt.Payload)
				if len(pcm) > 0 {
					s.recorder.RecordRemotePCM(pcm, recorderSampleRate)
				}
			}
		}
	}
	return nil
}

func (s *RecordingMediaSession) LocalAddr() net.Addr {
	return s.inner.LocalAddr()
}

func (s *RecordingMediaSession) RemoteAddr() net.Addr {
	return s.inner.RemoteAddr()
}

func (s *RecordingMediaSession) OnDTMF(handler voip.DTMFHandler) {
	s.inner.OnDTMF(handler)
}

// NegotiatedCodec delegates to the wrapped session (recording is transparent to
// codec negotiation).
func (s *RecordingMediaSession) NegotiatedCodec() voip.CodecInfo {
	return s.inner.NegotiatedCodec()
}

func (s *RecordingMediaSession) UnblockReaders() error {
	return s.inner.UnblockReaders()
}

func (s *RecordingMediaSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	inner := s.inner
	s.mu.Unlock()

	s.Finish()
	return inner.Close()
}

var _ voip.MediaSession = (*RecordingMediaSession)(nil)
