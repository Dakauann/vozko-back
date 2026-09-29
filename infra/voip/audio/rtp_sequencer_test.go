package audio

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

func packet(seq uint16) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq}}
}

func sequences(packets []*rtp.Packet) []uint16 {
	out := make([]uint16, 0, len(packets))
	for _, p := range packets {
		out = append(out, p.SequenceNumber)
	}
	return out
}

func assertSequences(t *testing.T, got []*rtp.Packet, want ...uint16) {
	t.Helper()
	seqs := sequences(got)
	if len(seqs) != len(want) {
		t.Fatalf("released %v, want %v", seqs, want)
	}
	for i := range want {
		if seqs[i] != want[i] {
			t.Fatalf("released %v, want %v", seqs, want)
		}
	}
}

func TestSequencerHoldsTheFirstPacketsUntilDepthIsExceeded(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	now := time.Now()
	assertSequences(t, s.Push(packet(10), now))
	assertSequences(t, s.Push(packet(11), now))
	assertSequences(t, s.Push(packet(12), now))
	assertSequences(t, s.Push(packet(13), now), 10, 11, 12, 13)
}

func TestSequencerReleasesContiguousPacketsImmediatelyOnceStarted(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	now := time.Now()
	s.Push(packet(10), now)
	assertSequences(t, s.Expire(now.Add(61*time.Millisecond)), 10)
	assertSequences(t, s.Push(packet(11), now), 11)
	assertSequences(t, s.Push(packet(12), now), 12)
}

func TestSequencerReordersPacketsWithinTheWindow(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	now := time.Now()
	s.Push(packet(1), now)
	s.Expire(now.Add(61 * time.Millisecond))
	assertSequences(t, s.Push(packet(3), now))
	assertSequences(t, s.Push(packet(2), now), 2, 3)
}

func TestSequencerSkipsAGapWhenTheWindowOverflows(t *testing.T) {
	s := newRTPSequencer(2, time.Second)
	now := time.Now()
	s.Push(packet(1), now)
	s.Expire(now.Add(2 * time.Second))
	assertSequences(t, s.Push(packet(3), now))
	assertSequences(t, s.Push(packet(4), now))
	assertSequences(t, s.Push(packet(5), now), 3, 4, 5)
}

func TestSequencerSkipsAGapAfterMaxWait(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	now := time.Now()
	s.Push(packet(1), now)
	s.Expire(now.Add(61 * time.Millisecond))
	s.Push(packet(3), now)
	assertSequences(t, s.Expire(now.Add(30*time.Millisecond)))
	assertSequences(t, s.Expire(now.Add(61*time.Millisecond)), 3)
}

func TestSequencerDropsLateAndDuplicatePackets(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	now := time.Now()
	s.Push(packet(5), now)
	s.Expire(now.Add(61 * time.Millisecond))
	assertSequences(t, s.Push(packet(4), now))
	assertSequences(t, s.Push(packet(5), now))
	s.Push(packet(7), now)
	assertSequences(t, s.Push(packet(7), now))
	assertSequences(t, s.Push(packet(6), now), 6, 7)
}

func TestSequencerHandlesSequenceWraparound(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	now := time.Now()
	s.Push(packet(65534), now)
	s.Expire(now.Add(61 * time.Millisecond))
	assertSequences(t, s.Push(packet(0), now))
	assertSequences(t, s.Push(packet(65535), now), 65535, 0)
}

func TestSequencerNextDeadlineTracksTheOldestHeldPacket(t *testing.T) {
	s := newRTPSequencer(3, 60*time.Millisecond)
	if _, ok := s.NextDeadline(); ok {
		t.Fatal("empty sequencer reported a deadline")
	}
	now := time.Now()
	s.Push(packet(1), now)
	s.Push(packet(2), now.Add(10*time.Millisecond))
	deadline, ok := s.NextDeadline()
	if !ok || !deadline.Equal(now.Add(60*time.Millisecond)) {
		t.Fatalf("NextDeadline() = %v, %v, want %v", deadline, ok, now.Add(60*time.Millisecond))
	}
}

func TestSequencerDrainReleasesEverythingInOrder(t *testing.T) {
	s := newRTPSequencer(5, time.Second)
	now := time.Now()
	s.Push(packet(3), now)
	s.Push(packet(1), now)
	s.Push(packet(2), now)
	assertSequences(t, s.Drain(), 1, 2, 3)
	assertSequences(t, s.Drain())
}
