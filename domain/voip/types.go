package voip

import (
	"fmt"
	"net"
	"time"

	"vozko/domain/calls/cdr"
)

type CallID string

type CallState string

const (
	CallStatePending CallState = "PENDING"
	CallStateRinging CallState = "RINGING"
	CallStateActive  CallState = "ACTIVE"
	CallStateEnded   CallState = "ENDED"
	CallStateFailed  CallState = "FAILED"
)

type DTMFHandler func(digit rune)

type MediaSession interface {
	ReadRTP(buf []byte, packet interface{}) (int, error)
	WriteRTP(packet interface{}) error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	Close() error

	OnDTMF(handler DTMFHandler)

	UnblockReaders() error

	// NegotiatedCodec reports the codec the SIP trunk negotiated for this call.
	// It is the single source of truth for which codec to encode/decode with, so
	// every channel (bridge, CRM, dialer, workflow) reads it from the session
	// rather than deciding independently, no two channels can diverge. Wrapping
	// sessions MUST delegate to the session they wrap.
	NegotiatedCodec() CodecInfo
}

type CallSession struct {
	ID           CallID
	PhoneNumber  string
	State        CallState
	StartedAt    time.Time
	LocalAddr    net.Addr
	RemoteAddr   net.Addr
	MediaSession MediaSession
	Media        MediaInfo

	TrunkHangup func() error

	// Direction marks the call as inbound (receptive) or outbound. Empty is
	// treated as outbound, preserving behavior for all existing originating
	// callers; the receptive inbound path sets it to cdr.DirectionInbound so the
	// CDR records the correct direction.
	Direction cdr.Direction
}

type StreamConfig struct {
	LocalPort   int
	RemoteAddr  string
	PayloadType int
	SSRC        uint32
	Codec       string
}

type StreamHandle interface{}

type InviteCallInput struct {
	PhoneNumber string
	Timeout     time.Duration
}

type HangupInput struct {
	CallID CallID
}

type CodecInfo struct {
	Name        string
	PayloadType uint8
	SampleRate  uint32
	Channels    int
	PtimeMs     int
}

type MediaInfo struct {
	Codecs         []CodecInfo
	SelectedCodec  CodecInfo
	LocalRTPAddr   string
	RemoteRTPAddr  string
	LocalRTCPAddr  string
	RemoteRTCPAddr string
	Mode           string
	RTPProfile     string
	SecureRTP      bool
}

type RecordingMeta struct {
	CallID      string
	WorkspaceID string
	EntryID     string
	LeadID      string
}

type MediaSessionRecorder func(inner MediaSession, dialogID string, meta RecordingMeta) MediaSession

func (m MediaInfo) String() string {
	if m.SelectedCodec.Name == "" {
		return "MediaInfo{empty}"
	}
	return fmt.Sprintf("MediaInfo{codec=%s(PT:%d) rate=%dHz ch=%d ptime=%dms mode=%s local=%s remote=%s secure=%v}",
		m.SelectedCodec.Name,
		m.SelectedCodec.PayloadType,
		m.SelectedCodec.SampleRate,
		m.SelectedCodec.Channels,
		m.SelectedCodec.PtimeMs,
		m.Mode,
		m.LocalRTPAddr,
		m.RemoteRTPAddr,
		m.SecureRTP,
	)
}
