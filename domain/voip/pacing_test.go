package voip

import (
	"errors"
	"testing"
	"time"
)

func TestPacePCMSendsFramesInRealTime(t *testing.T) {
	var frames [][]byte
	start := time.Now()
	interrupted, err := PacePCM(make([]byte, 4*PCMFrameBytes+10), func(frame []byte) error {
		frames = append(frames, frame)
		return nil
	}, make(chan struct{}), func() bool { return false })
	if err != nil || interrupted {
		t.Fatalf("PacePCM = %v, %v", interrupted, err)
	}
	if len(frames) != 5 || len(frames[4]) != 10 {
		t.Fatalf("frames = %d, last = %d bytes", len(frames), len(frames[len(frames)-1]))
	}
	if time.Since(start) < 4*PCMFrameDuration {
		t.Fatal("frames went out faster than real time")
	}
}

func TestPacePCMStopsWhenInterruptedOrStopped(t *testing.T) {
	sent := 0
	interrupted, err := PacePCM(make([]byte, 50*PCMFrameBytes), func([]byte) error { sent++; return nil }, make(chan struct{}), func() bool { return sent == 2 })
	if err != nil || !interrupted || sent != 2 {
		t.Fatalf("interrupt: %v %v sent=%d", interrupted, err, sent)
	}

	stop := make(chan struct{})
	close(stop)
	if _, err := PacePCM(make([]byte, 50*PCMFrameBytes), func([]byte) error { return nil }, stop, func() bool { return false }); !errors.Is(err, ErrPlaybackStopped) {
		t.Fatalf("stop err = %v", err)
	}
}

func TestLoopPCMRepeatsUntilStopped(t *testing.T) {
	stop := make(chan struct{})
	sent := 0
	err := LoopPCM(make([]byte, 2*PCMFrameBytes), func([]byte) error {
		sent++
		if sent == 5 {
			close(stop)
		}
		return nil
	}, stop)
	if !errors.Is(err, ErrPlaybackStopped) || sent < 5 {
		t.Fatalf("LoopPCM = %v after %d frames", err, sent)
	}
	if err := LoopPCM(nil, func([]byte) error { return nil }, make(chan struct{})); !errors.Is(err, ErrNothingToPlay) {
		t.Fatalf("empty loop err = %v", err)
	}
}
