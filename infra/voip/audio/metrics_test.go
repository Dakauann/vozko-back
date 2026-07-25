package audio

import (
	"context"
	"errors"
	"testing"
)

func TestStreamMetricsCountsFrames(t *testing.T) {
	media := &capturingMedia{}
	s := New(media, Options{SampleRate: DefaultSampleRate, FrameDur: DefaultFrameDur})

	// One full audio frame, one silence frame (no data).
	if err := s.WritePCM16(context.Background(), ramp(DefaultFrameBytes)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = s.Step() // audio frame
	_ = s.Step() // silence frame (buffer now empty)

	m := s.Metrics()
	if m.AudioFrames != 1 {
		t.Fatalf("AudioFrames = %d, want 1", m.AudioFrames)
	}
	if m.SilenceFrames != 1 {
		t.Fatalf("SilenceFrames = %d, want 1", m.SilenceFrames)
	}
	if m.BytesEnqueued != uint64(DefaultFrameBytes) {
		t.Fatalf("BytesEnqueued = %d, want %d", m.BytesEnqueued, DefaultFrameBytes)
	}
	if m.BytesEmitted != uint64(DefaultFrameBytes) {
		t.Fatalf("BytesEmitted = %d, want %d", m.BytesEmitted, DefaultFrameBytes)
	}
	if m.LastWriteRTPErr != nil {
		t.Fatalf("unexpected write error: %v", m.LastWriteRTPErr)
	}
}

func TestStreamMetricsCapturesWriteError(t *testing.T) {
	wantErr := errors.New("boom")
	media := &capturingMedia{err: wantErr}
	s := New(media, Options{SampleRate: DefaultSampleRate, FrameDur: DefaultFrameDur})

	_ = s.Step() // emits a silence frame; WriteRTP fails

	m := s.Metrics()
	if m.WriteRTPErrors != 1 {
		t.Fatalf("WriteRTPErrors = %d, want 1", m.WriteRTPErrors)
	}
	if m.LastWriteRTPErr == nil || m.LastWriteRTPErr.Error() != "boom" {
		t.Fatalf("LastWriteRTPErr = %v, want boom", m.LastWriteRTPErr)
	}
}
