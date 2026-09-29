package audio

import (
	"time"

	"github.com/pion/rtp"
)

type heldPacket struct {
	packet  *rtp.Packet
	arrived time.Time
}

type rtpSequencer struct {
	depth   int
	maxWait time.Duration
	started bool
	next    uint16
	held    []heldPacket
}

func newRTPSequencer(depth int, maxWait time.Duration) *rtpSequencer {
	return &rtpSequencer{depth: depth, maxWait: maxWait}
}

func seqBefore(a, b uint16) bool {
	return int16(a-b) < 0
}

func (s *rtpSequencer) Push(p *rtp.Packet, now time.Time) []*rtp.Packet {
	seq := p.SequenceNumber
	if s.started && seqBefore(seq, s.next) {
		return nil
	}
	at := len(s.held)
	for i, h := range s.held {
		if h.packet.SequenceNumber == seq {
			return nil
		}
		if seqBefore(seq, h.packet.SequenceNumber) {
			at = i
			break
		}
	}
	s.held = append(s.held, heldPacket{})
	copy(s.held[at+1:], s.held[at:])
	s.held[at] = heldPacket{packet: p, arrived: now}

	released := s.releaseContiguous(nil)
	for len(s.held) > s.depth {
		released = s.releaseContiguous(append(released, s.releaseHead()))
	}
	return released
}

func (s *rtpSequencer) Expire(now time.Time) []*rtp.Packet {
	var released []*rtp.Packet
	for {
		deadline, ok := s.NextDeadline()
		if !ok || now.Before(deadline) {
			return released
		}
		released = s.releaseContiguous(append(released, s.releaseHead()))
	}
}

func (s *rtpSequencer) NextDeadline() (time.Time, bool) {
	if len(s.held) == 0 {
		return time.Time{}, false
	}
	oldest := s.held[0].arrived
	for _, h := range s.held[1:] {
		if h.arrived.Before(oldest) {
			oldest = h.arrived
		}
	}
	return oldest.Add(s.maxWait), true
}

func (s *rtpSequencer) Drain() []*rtp.Packet {
	var released []*rtp.Packet
	for len(s.held) > 0 {
		released = append(released, s.releaseHead())
	}
	return released
}

func (s *rtpSequencer) releaseHead() *rtp.Packet {
	head := s.held[0].packet
	s.held = s.held[1:]
	s.started = true
	s.next = head.SequenceNumber + 1
	return head
}

func (s *rtpSequencer) releaseContiguous(released []*rtp.Packet) []*rtp.Packet {
	for s.started && len(s.held) > 0 && s.held[0].packet.SequenceNumber == s.next {
		released = append(released, s.releaseHead())
	}
	return released
}
