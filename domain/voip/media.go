package voip

import (
	"errors"
	"net"
)

const PCMSampleRate = 8000

var ErrCodecNotBridgeable = errors.New("negotiated codec cannot be bridged to the browser; enable PCMU or PCMA on the trunk")

type MediaSession interface {
	ReadRTP(buf []byte, packet any) (int, error)
	WriteRTP(packet any) error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	Close() error
}

type PCMStream interface {
	Frames() <-chan []byte
	WritePCM(pcm []byte) error
	Done() <-chan struct{}
	Close() error
}

type DTMFHandler func(digit rune)

type DTMFSource interface {
	OnDTMF(handler DTMFHandler)
}

type CodecReporter interface {
	NegotiatedCodec() CodecInfo
}

type ReaderUnblocker interface {
	UnblockReaders() error
}

type CodecInfo struct {
	Name        string
	PayloadType uint8
	SampleRate  uint32
	Channels    int
	PtimeMs     int
}

type MediaInfo struct {
	Codec           CodecInfo
	DTMFPayloadType uint8
	Encrypted       bool
}
