package dialer

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

func TestLoopingPCMSource_FramesAndWrap(t *testing.T) {
	t.Parallel()
	// 1.5 frames of ascending samples: NextFrame must yield full 320-byte frames
	// and wrap seamlessly back to the start.
	pcm := make([]byte, sipBytesPerFrame+sipBytesPerFrame/2)
	for i := 0; i < len(pcm)/2; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(i))
	}
	src := NewLoopingPCMSource(pcm)
	if src == nil {
		t.Fatal("source must be built for a >= 1 frame buffer")
	}

	f1 := append([]byte(nil), src.NextFrame()...)
	if len(f1) != sipBytesPerFrame {
		t.Fatalf("frame size = %d, want %d", len(f1), sipBytesPerFrame)
	}
	if !bytes.Equal(f1, pcm[:sipBytesPerFrame]) {
		t.Fatal("first frame must be the buffer's first 320 bytes")
	}
	f2 := append([]byte(nil), src.NextFrame()...)
	want := append(append([]byte(nil), pcm[sipBytesPerFrame:]...), pcm[:sipBytesPerFrame/2]...)
	if !bytes.Equal(f2, want) {
		t.Fatal("second frame must wrap: tail of the loop followed by its head")
	}
}

func TestLoopingPCMSource_TooShortFallsBackToNil(t *testing.T) {
	t.Parallel()
	if src := NewLoopingPCMSource(make([]byte, sipBytesPerFrame-2)); src != nil {
		t.Fatal("a sub-frame buffer must yield a nil source (silence fallback), not a stutter")
	}
}

func TestGeneratedSources_ProduceRealFrames(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]HoldAudioSource{
		"comfort":  NewComfortToneSource(),
		"ringback": NewRingbackToneSource(),
	} {
		if src == nil {
			t.Fatalf("%s: nil source", name)
		}
		nonSilent := false
		for i := 0; i < 300; i++ { // 6s of frames covers each loop's audible burst
			f := src.NextFrame()
			if len(f) != sipBytesPerFrame {
				t.Fatalf("%s: frame %d size=%d", name, i, len(f))
			}
			for j := 0; j < len(f); j += 2 {
				if int16(binary.LittleEndian.Uint16(f[j:])) != 0 {
					nonSilent = true
					break
				}
			}
		}
		if !nonSilent {
			t.Fatalf("%s: generated loop is pure silence, the whole point is to kill dead air", name)
		}
	}
}

func TestHoldPlayer_PumpsSourceFramesAndStops(t *testing.T) {
	t.Parallel()
	sink := &countingSink{}
	src := NewLoopingPCMSource(bytes.Repeat([]byte{0x01, 0x02}, sipBytesPerFrame)) // 2 frames
	p := NewHoldPlayer(sink, src)
	p.Start(context.Background())

	deadline := time.After(2 * time.Second)
	for sink.frames.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("player pumped only %d frames", sink.frames.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	p.Stop()
	frozen := sink.frames.Load()
	time.Sleep(60 * time.Millisecond)
	if got := sink.frames.Load(); got != frozen {
		t.Fatalf("player kept pumping after Stop: %d -> %d", frozen, got)
	}
	p.Stop() // idempotent
}

func TestHoldPlayer_NilSourcePlaysSilence(t *testing.T) {
	t.Parallel()
	sink := &captureSink{}
	p := NewHoldPlayer(sink, nil)
	p.Start(context.Background())
	deadline := time.After(2 * time.Second)
	for sink.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("no frames pumped")
		case <-time.After(10 * time.Millisecond):
		}
	}
	p.Stop()
	for _, f := range sink.all() {
		if len(f) != sipBytesPerFrame {
			t.Fatalf("silence frame size = %d", len(f))
		}
		for _, b := range f {
			if b != 0 {
				t.Fatal("nil source must play pure silence")
			}
		}
	}
}

type captureSink struct {
	mu     sync.Mutex
	frames [][]byte
}

func (s *captureSink) SendAudio(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), pcm...)
	s.frames = append(s.frames, cp)
	return nil
}

func (s *captureSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

func (s *captureSink) all() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.frames))
	copy(out, s.frames)
	return out
}
