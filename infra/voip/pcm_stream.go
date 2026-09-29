package voipinfra

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/pion/rtp"
	"github.com/zaf/g711"

	"vozko/domain/voip"
)

const (
	pcmFrameSamples = voip.PCMSampleRate / 50
	pcmFrameBytes   = pcmFrameSamples * 2
	pcmFrameQueue   = 50
)

type g711Codec struct {
	payloadType uint8
	encode      func([]byte) []byte
	decode      func([]byte) []byte
}

func bridgeableCodec(codec voip.CodecInfo) (g711Codec, error) {
	if codec.SampleRate == voip.PCMSampleRate {
		switch strings.ToUpper(codec.Name) {
		case "PCMU":
			return g711Codec{payloadType: codec.PayloadType, encode: g711.EncodeUlaw, decode: g711.DecodeUlaw}, nil
		case "PCMA":
			return g711Codec{payloadType: codec.PayloadType, encode: g711.EncodeAlaw, decode: g711.DecodeAlaw}, nil
		}
	}
	return g711Codec{}, fmt.Errorf("%w: %q", voip.ErrCodecNotBridgeable, codec.Name)
}

type pcmStream struct {
	media  voip.MediaSession
	codec  g711Codec
	frames chan []byte
	done   chan struct{}

	writeMu   sync.Mutex
	pending   []byte
	sequence  uint16
	timestamp uint32
	ssrc      uint32
	started   bool
	closed    bool

	closeOnce sync.Once
	closeErr  error
}

func newPCMStream(media voip.MediaSession, codecInfo voip.CodecInfo) (*pcmStream, error) {
	codec, err := bridgeableCodec(codecInfo)
	if err != nil {
		return nil, err
	}
	var seed [10]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, fmt.Errorf("seed RTP stream: %w", err)
	}
	s := &pcmStream{
		media:     media,
		codec:     codec,
		frames:    make(chan []byte, pcmFrameQueue),
		done:      make(chan struct{}),
		ssrc:      binary.BigEndian.Uint32(seed[0:4]),
		sequence:  binary.BigEndian.Uint16(seed[4:6]),
		timestamp: binary.BigEndian.Uint32(seed[6:10]),
	}
	go s.readLoop()
	return s, nil
}

func (s *pcmStream) readLoop() {
	defer close(s.frames)
	defer s.markDone()
	buf := make([]byte, 1500)
	for {
		var packet rtp.Packet
		if _, err := s.media.ReadRTP(buf, &packet); err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			return
		}
		if packet.PayloadType != s.codec.payloadType || len(packet.Payload) == 0 {
			continue
		}
		s.deliver(s.codec.decode(packet.Payload))
	}
}

func (s *pcmStream) deliver(frame []byte) {
	for {
		select {
		case s.frames <- frame:
			return
		default:
		}
		select {
		case <-s.frames:
		default:
		}
	}
}

func (s *pcmStream) markDone() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
}

func (s *pcmStream) Frames() <-chan []byte {
	return s.frames
}

func (s *pcmStream) Done() <-chan struct{} {
	return s.done
}

func (s *pcmStream) WritePCM(pcm []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	s.pending = append(s.pending, pcm...)
	for len(s.pending) >= pcmFrameBytes {
		frame := s.pending[:pcmFrameBytes]
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Marker:         !s.started,
				PayloadType:    s.codec.payloadType,
				SequenceNumber: s.sequence,
				Timestamp:      s.timestamp,
				SSRC:           s.ssrc,
			},
			Payload: s.codec.encode(frame),
		}
		s.pending = s.pending[pcmFrameBytes:]
		s.started = true
		s.sequence++
		s.timestamp += pcmFrameSamples
		if err := s.media.WriteRTP(packet); err != nil {
			return err
		}
	}
	if len(s.pending) == 0 {
		s.pending = nil
	}
	return nil
}

func (s *pcmStream) Close() error {
	s.closeOnce.Do(func() {
		s.markDone()
		s.closeErr = s.media.Close()
	})
	return s.closeErr
}
